{
  description = "CLI that authenticates against Microsoft Entra ID and prints an OAuth2 access token";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = self.shortRev or self.dirtyShortRev or "dev";

      mkEntraHelper =
        pkgs:
        pkgs.buildGoModule {
          pname = "entra-helper";
          inherit version;

          src = self;
          subPackages = [ "." ];

          # If this goes stale after a dependency bump, run `nix build`,
          # copy the "got:" hash from the mismatch error, and paste it here.
          vendorHash = "sha256-geh5Peo1VPXb15Gy+FQ3grjMEOxgo8WPHUv+aPobszc=";

          # The keychain-backed token cache needs cgo on darwin; linux builds
          # are pure Go (matches the GoReleaser release configuration).
          env.CGO_ENABLED = if pkgs.stdenv.hostPlatform.isDarwin then "1" else "0";

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
            "-X main.commit=${self.rev or self.dirtyRev or "unknown"}"
          ];

          meta = {
            description = "Authenticate against Microsoft Entra ID and print an OAuth2 access token";
            homepage = "https://github.com/illumination-k/entra-helper";
            mainProgram = "entra-helper";
          };
        };
    in
    {
      overlays.default = final: _prev: {
        entra-helper = mkEntraHelper final;
      };

      packages = forAllSystems (pkgs: rec {
        entra-helper = mkEntraHelper pkgs;
        default = entra-helper;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            golangci-lint
            govulncheck
            goreleaser
            dprint
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
