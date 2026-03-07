{
  description = "A Go 1.25 development environment";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  # Use flake-utils to simplify flake setup across multiple systems
  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      ...
    }@inputs:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        # Import the nixpkgs with overlay and configuration
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ ];
        };
      in
      {
        # Define a development shell
        devShell = pkgs.mkShell {
          buildInputs = [
            pkgs.go
            pkgs.gopls
            pkgs.gotools # Includes godoc
          ];

          # Optional: Set GOPATH and other env variables
          shellHook = ''
            echo "Dev environment for claude-statusline"
            echo "=================================="
            go version
            echo ""
            echo "make commands for claude-statusline:"
            echo "  make build       - Builds claude-statusline binary in working dir"
            echo "  make install     - Runs build first, then copies binary to ~/.claude/claude-statusline"
            echo "  make test        - Runs build first, then executes test.sh"
            echo "  make clean       - Deletes the compiled binary from working dir"
            echo ""
          '';
        };
      }
    );
}
