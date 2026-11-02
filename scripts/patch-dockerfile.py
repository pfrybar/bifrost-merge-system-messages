#!/usr/bin/env python3
"""Turn upstream Bifrost's transports/Dockerfile.local into a plugin-capable Dockerfile.

Usage: patch-dockerfile.py <Dockerfile.local> <output Dockerfile>

Upstream builds a statically linked binary, which cannot load Go .so plugins. This
script rewrites the build so that Bifrost is linked dynamically and the plugin is built
in the same stage with the same toolchain and flags (Go refuses to load a plugin built
any other way), then adds the plugin to the runtime image.

Every edit first checks that the expected upstream text is present. If upstream changes
its Dockerfile layout, the script exits with an error instead of producing an image
whose plugin silently fails to load.
"""

import sys
from pathlib import Path

PLUGIN_DIR = "mergeleadingsystemmessages"
PLUGIN_SO = "merge-leading-system-messages.so"


def fail(message: str) -> None:
    raise SystemExit(f"patch-dockerfile: {message}")


def replace_once(dockerfile: str, old: str, new: str, what: str) -> str:
    if dockerfile.count(old) != 1:
        fail(f"upstream Dockerfile.local no longer has the expected {what}")
    return dockerfile.replace(old, new, 1)


def patch(dockerfile: str) -> str:
    # 1. Go workspace. Since v2.x upstream adds every plugins/*/go.mod automatically,
    #    which includes the copied plugin. Older layouts listed modules explicitly.
    if "for plugin_dir in ./plugins/*/; do" not in dockerfile:
        marker = "    go work use ./transports"
        dockerfile = replace_once(
            dockerfile,
            marker,
            f"    go work use ./plugins/{PLUGIN_DIR} && \\\n" + marker,
            "workspace setup",
        )

    # 2. Static build -> dynamic build of Bifrost plus the plugin.
    static_build = """  # Build the binary with CGO enabled and static SQLite linking
  ARG VERSION=unknown
  RUN cd /build/transports && \\
    go build \\
      -ldflags="-w -s -X main.Version=v${VERSION} -extldflags '-static'" \\
      -a -trimpath \\
      -tags "sqlite_static" \\
      -o /app/main \\
      ./bifrost-http"""
    dynamic_build = f"""  # Build Bifrost and the plugin with identical Go, libc, source, and trimpath settings.
  # Go shared-object plugins require a dynamically linked host binary.
  ARG VERSION=unknown
  RUN cd /build/transports && \\
    go build \\
      -ldflags="-w -s -X main.Version=v${{VERSION}}" \\
      -a -trimpath \\
      -o /app/main \\
      ./bifrost-http && \\
    cd /build/plugins/{PLUGIN_DIR} && \\
    go build \\
      -buildmode=plugin \\
      -a -trimpath \\
      -o /app/{PLUGIN_SO} \\
      ."""
    dockerfile = replace_once(dockerfile, static_build, dynamic_build, "static build command")

    # 3. The dynamically linked binary needs the SQLite shared library at runtime.
    runtime_deps = "apk add --no-cache musl libgcc ca-certificates"
    dockerfile = replace_once(dockerfile, runtime_deps, runtime_deps + " sqlite-libs", "runtime dependencies")

    # 4. Ship the plugin next to the binary.
    binary_copy = "  COPY --from=builder /app/main ."
    dockerfile = replace_once(
        dockerfile,
        binary_copy,
        binary_copy + f"\n  COPY --from=builder /app/{PLUGIN_SO} /app/plugins/{PLUGIN_SO}",
        "binary copy",
    )
    return dockerfile


def main() -> None:
    if len(sys.argv) != 3:
        fail("usage: patch-dockerfile.py <Dockerfile.local> <output Dockerfile>")
    source, output = Path(sys.argv[1]), Path(sys.argv[2])
    output.write_text(patch(source.read_text()))


if __name__ == "__main__":
    main()
