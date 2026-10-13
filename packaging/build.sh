#!/bin/sh
# Runs inside the packaging/Dockerfile image with the repo mounted at /src.
# Builds ducktainer with PAM (cgo) for each arch and packs rpm + deb.
set -eu
: "${VERSION:=0.1.0}"
: "${MAINTAINER:=ducktainer <ducktainer@localhost>}"
: "${ARCHES:=amd64 arm64}"
export HOME=/tmp GOCACHE=/tmp/gocache GOPATH=/tmp/gopath GOFLAGS=-buildvcs=false
mkdir -p dist/build dist/packages
for arch in $ARCHES; do
	case $arch in
	amd64) cc=gcc ;;
	arm64) cc=aarch64-linux-gnu-gcc ;;
	*) echo "arquitectura no soportada: $arch" >&2; exit 1 ;;
	esac
	echo "→ go build linux/$arch (cgo + PAM)"
	CGO_ENABLED=1 GOOS=linux GOARCH=$arch CC=$cc \
		go build -trimpath -tags pam -ldflags "-s -w -X main.version=$VERSION" -o "dist/build/ducktainer-linux-$arch" .
	# nfpm doesn't expand variables in content paths: render per arch
	sed "s|\${ARCH}|$arch|g" packaging/nfpm.yaml > "/tmp/nfpm-$arch.yaml"
	for fmt in rpm deb; do
		ARCH=$arch VERSION=$VERSION MAINTAINER=$MAINTAINER nfpm package -f "/tmp/nfpm-$arch.yaml" -p $fmt -t dist/packages/
	done
done
ls -l dist/packages
# guard: the highest glibc symbol version required must stay ≤ 2.34 (RHEL 9)
for f in dist/build/ducktainer-linux-*; do
	max=$(strings "$f" | grep -o 'GLIBC_2\.[0-9]*' | sort -t. -k2 -n | tail -1)
	echo "$f requiere $max"
done
