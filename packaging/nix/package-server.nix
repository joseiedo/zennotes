{
  lib,
  buildGoModule,
  fetchFromGitHub,
  buildNpmPackage,
}:
let
  releaseData = lib.importJSON ./release-data.json;

  src = fetchFromGitHub {
    owner = "ZenNotes";
    repo = "zennotes";
    tag = "v${releaseData.version}";
    inherit (releaseData) hash;
  };

  web = buildNpmPackage {
    pname = "zennotes-web";

    inherit (releaseData) version npmDepsHash;
    inherit src;

    npmWorkspace = "apps/web";

    env.ELECTRON_SKIP_BINARY_DOWNLOAD = "1";

    installPhase = ''
      runHook preInstall

      # Both bundles the server embeds: the admin app (web/dist) and the
      # public-site reader (web/dist-public, required by go:embed).
      mkdir -p "$out/dist" "$out/dist-public"
      cp -R apps/web/dist/. "$out/dist/"
      cp -R apps/web/dist-public/. "$out/dist-public/"

      runHook postInstall
    '';
  };
in
buildGoModule (finalAttrs: {
  pname = "zennotes-server";

  inherit (releaseData) version vendorHash;
  inherit src;

  modRoot = "apps/server";

  subPackages = [ "cmd/zennotes-server" ];
  ldflags = [
    "-s"
    "-w"
  ];

  preBuild = ''
    rm -rf web/dist web/dist-public
    mkdir -p web/dist web/dist-public
    cp -R ${web}/dist/. web/dist/
    cp -R ${web}/dist-public/. web/dist-public/
  '';

  meta = {
    description = "A server API for hosting remote ZenNotes vaults";
    homepage = "https://zennotes.org/";
    changelog = "https://github.com/ZenNotes/zennotes/releases/tag/v${finalAttrs.version}";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ justkrysteq ];
    mainProgram = finalAttrs.pname;
    platforms = lib.platforms.linux ++ lib.platforms.darwin;
  };
})
