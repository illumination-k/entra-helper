// Command entra-helper authenticates against Microsoft Entra ID using one of
// several credential flows and prints the resulting OAuth2 access token.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity/cache"
	"github.com/spf13/cobra"
)

const authTimeout = 5 * time.Minute

// Build metadata, injected via -ldflags by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	tenantID  string
	clientID  string
	scopes    []string
	expiresOn bool
	noCache   bool
	record    string
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "entra-helper",
		Short:         "Print a Microsoft Entra ID access token",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&opts.tenantID, "tenant-id", os.Getenv("AZURE_TENANT_ID"), "Entra tenant ID")
	pf.StringVar(&opts.clientID, "client-id", os.Getenv("AZURE_CLIENT_ID"), "application (client) ID")
	pf.StringSliceVar(&opts.scopes, "scope", nil, "token scope, required (repeatable or comma-separated)")
	pf.BoolVar(&opts.expiresOn, "expires-on", false, "print token expiry to stderr")
	pf.BoolVar(&opts.noCache, "no-cache", false, "disable the persistent token cache")
	pf.StringVar(&opts.record, "record", defaultRecordPath(), "path to persist the authentication record")

	root.AddCommand(
		newAuthCmd("interactive", "Authenticate via the interactive browser flow", opts),
		newAuthCmd("devicecode", "Authenticate via the device code flow", opts),
		newAuthCmd("default", "Authenticate via DefaultAzureCredential (env, managed identity, az cli, ...)", opts),
	)
	cobra.CheckErr(root.MarkPersistentFlagRequired("scope"))
	return root
}

func newAuthCmd(kind, short string, opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   kind,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuth(cmd.Context(), kind, opts)
		},
	}
}

// authenticator is implemented by the interactive and device-code credentials,
// which can persist their state to a cache and emit an AuthenticationRecord.
type authenticator interface {
	azcore.TokenCredential
	Authenticate(ctx context.Context, opts *policy.TokenRequestOptions) (azidentity.AuthenticationRecord, error)
}

func runAuth(parent context.Context, kind string, opts *options) error {
	ctx, cancel := context.WithTimeout(parent, authTimeout)
	defer cancel()

	scopes := opts.scopes
	persist := !opts.noCache && kind != "default"

	var (
		tokenCache azidentity.Cache
		record     azidentity.AuthenticationRecord
		haveRecord bool
	)
	if persist {
		c, err := cache.New(nil)
		if err != nil {
			return fmt.Errorf("opening token cache: %w", err)
		}
		tokenCache = c
		if rec, err := loadRecord(opts.record); err == nil && rec != (azidentity.AuthenticationRecord{}) {
			record = rec
			haveRecord = true
		}
	}

	cred, err := buildCredential(kind, opts, tokenCache, record)
	if err != nil {
		return err
	}

	// Only trigger the interactive prompt when we have no cached
	// AuthenticationRecord yet. Once one is persisted, subsequent runs reuse
	// the cache and GetToken refreshes the token silently.
	if a, ok := cred.(authenticator); ok && persist && !haveRecord {
		if err = authenticateAndSave(ctx, a, opts.record, scopes); err != nil {
			return err
		}
	}

	tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: scopes})
	if err != nil {
		// A persisted record exists but the silent refresh failed (e.g. the
		// refresh token expired or was revoked). Fall back to a fresh
		// interactive authentication once before giving up.
		if a, ok := cred.(authenticator); ok && persist && haveRecord {
			if authErr := authenticateAndSave(ctx, a, opts.record, scopes); authErr != nil {
				return authErr
			}
			tok, err = cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: scopes})
		}
		if err != nil {
			return fmt.Errorf("acquiring token: %w", err)
		}
	}

	fmt.Println(tok.Token)
	if opts.expiresOn {
		fmt.Fprintln(os.Stderr, "expires-on:", tok.ExpiresOn.Format(time.RFC3339))
	}
	return nil
}

// authenticateAndSave runs the credential's interactive authentication and
// persists the resulting AuthenticationRecord so later runs can refresh
// silently.
func authenticateAndSave(ctx context.Context, a authenticator, recordPath string, scopes []string) error {
	rec, err := a.Authenticate(ctx, &policy.TokenRequestOptions{Scopes: scopes})
	if err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}
	if err := saveRecord(recordPath, rec); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not persist auth record:", err)
	}
	return nil
}

func buildCredential(kind string, o *options, c azidentity.Cache, rec azidentity.AuthenticationRecord) (azcore.TokenCredential, error) {
	switch kind {
	case "interactive":
		return azidentity.NewInteractiveBrowserCredential(&azidentity.InteractiveBrowserCredentialOptions{
			TenantID:             o.tenantID,
			ClientID:             o.clientID,
			Cache:                c,
			AuthenticationRecord: rec,
		})
	case "devicecode":
		return azidentity.NewDeviceCodeCredential(&azidentity.DeviceCodeCredentialOptions{
			TenantID:             o.tenantID,
			ClientID:             o.clientID,
			Cache:                c,
			AuthenticationRecord: rec,
			// Print the prompt to stderr so stdout stays clean for the token.
			UserPrompt: func(_ context.Context, m azidentity.DeviceCodeMessage) error {
				fmt.Fprintln(os.Stderr, m.Message)
				return nil
			},
		})
	case "default":
		return azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{
			TenantID: o.tenantID,
		})
	default:
		return nil, fmt.Errorf("unknown credential flow %q", kind)
	}
}

func defaultRecordPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "entra-helper", "auth-record.json")
}

func loadRecord(path string) (azidentity.AuthenticationRecord, error) {
	var rec azidentity.AuthenticationRecord
	if path == "" {
		return rec, fmt.Errorf("no record path configured")
	}
	b, err := os.ReadFile(path) //nolint:gosec // path is an explicit, user-supplied CLI flag
	if err != nil {
		return rec, err
	}
	err = json.Unmarshal(b, &rec)
	return rec, err
}

func saveRecord(path string, rec azidentity.AuthenticationRecord) error {
	if path == "" {
		return fmt.Errorf("no record path configured")
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
