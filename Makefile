NAME=mihomo
BINDIR=bin
BRANCH=$(shell git branch --show-current)
ifeq ($(BRANCH),Alpha)
VERSION=alpha-$(shell git rev-parse --short HEAD)
else ifeq ($(BRANCH),Beta)
VERSION=beta-$(shell git rev-parse --short HEAD)
else ifeq ($(BRANCH),)
VERSION=$(shell git describe --tags)
else
VERSION=$(shell git rev-parse --short HEAD)
endif

BUILDTIME=$(shell date -u)
GOBUILD=CGO_ENABLED=0 go build -tags with_gvisor -trimpath -ldflags '-X "github.com/metacubex/mihomo/constant.Version=$(VERSION)" \
		-X "github.com/metacubex/mihomo/constant.BuildTime=$(BUILDTIME)" \
		-w -s -buildid='

PLATFORM_LIST = \
	darwin-386 \
	darwin-amd64-compatible \
	darwin-amd64 \
	darwin-amd64-v1 \
	darwin-amd64-v2 \
	darwin-amd64-v3 \
	darwin-arm64 \
	linux-386 \
	linux-amd64-compatible \
	linux-amd64 \
	linux-amd64-v1 \
	linux-amd64-v2 \
	linux-amd64-v3 \
	linux-armv5 \
	linux-armv6 \
	linux-armv7 \
	linux-arm64 \
	linux-mips64 \
	linux-mips64le \
	linux-mips-softfloat \
	linux-mips-hardfloat \
	linux-mipsle-softfloat \
	linux-mipsle-hardfloat \
	linux-riscv64 \
	linux-loong64 \
	android-arm64 \
	freebsd-386 \
	freebsd-amd64 \
	freebsd-arm64

WINDOWS_ARCH_LIST = \
	windows-386 \
	windows-amd64-compatible \
	windows-amd64 \
	windows-amd64-v1 \
	windows-amd64-v2 \
	windows-amd64-v3 \
	windows-arm64 \
	windows-arm32v7

all: linux-amd64-v3 linux-arm64 \
	darwin-amd64-v3 darwin-arm64 \
	windows-amd64-v3 windows-arm64

darwin-all: darwin-amd64-v3 darwin-arm64

docker:
	GOAMD64=v1 $(GOBUILD) -o $(BINDIR)/$(NAME)-$@

# Parameterized cross-compile rule:
#   $(1) target name, $(2) GOARCH, $(3) GOOS,
#   $(4) extra GO env (GOAMD64/GOARM/GOMIPS), $(5) output file suffix
define platform_rule
$(1):
	GOARCH=$(2) GOOS=$(3)$(if $(4), $(4),) $$(GOBUILD) -o $$(BINDIR)/$$(NAME)-$$@$(5)

endef

$(eval $(call platform_rule,darwin-386,386,darwin))
$(eval $(call platform_rule,darwin-amd64-compatible,amd64,darwin,GOAMD64=v1))
$(eval $(call platform_rule,darwin-amd64,amd64,darwin,GOAMD64=v3))
$(eval $(call platform_rule,darwin-amd64-v1,amd64,darwin,GOAMD64=v1))
$(eval $(call platform_rule,darwin-amd64-v2,amd64,darwin,GOAMD64=v2))
$(eval $(call platform_rule,darwin-amd64-v3,amd64,darwin,GOAMD64=v3))
$(eval $(call platform_rule,darwin-arm64,arm64,darwin))
$(eval $(call platform_rule,linux-386,386,linux))
$(eval $(call platform_rule,linux-amd64-compatible,amd64,linux,GOAMD64=v1))
$(eval $(call platform_rule,linux-amd64,amd64,linux,GOAMD64=v3))
$(eval $(call platform_rule,linux-amd64-v1,amd64,linux,GOAMD64=v1))
$(eval $(call platform_rule,linux-amd64-v2,amd64,linux,GOAMD64=v2))
$(eval $(call platform_rule,linux-amd64-v3,amd64,linux,GOAMD64=v3))
$(eval $(call platform_rule,linux-arm64,arm64,linux))
$(eval $(call platform_rule,linux-armv5,arm,linux,GOARM=5))
$(eval $(call platform_rule,linux-armv6,arm,linux,GOARM=6))
$(eval $(call platform_rule,linux-armv7,arm,linux,GOARM=7))
$(eval $(call platform_rule,linux-mips64,mips64,linux))
$(eval $(call platform_rule,linux-mips64le,mips64le,linux))
$(eval $(call platform_rule,linux-mips-softfloat,mips,linux,GOMIPS=softfloat))
$(eval $(call platform_rule,linux-mips-hardfloat,mips,linux,GOMIPS=hardfloat))
$(eval $(call platform_rule,linux-mipsle-softfloat,mipsle,linux,GOMIPS=softfloat))
$(eval $(call platform_rule,linux-mipsle-hardfloat,mipsle,linux,GOMIPS=hardfloat))
$(eval $(call platform_rule,linux-riscv64,riscv64,linux))
$(eval $(call platform_rule,linux-loong64,loong64,linux))
$(eval $(call platform_rule,android-arm64,arm64,android))
$(eval $(call platform_rule,freebsd-386,386,freebsd))
$(eval $(call platform_rule,freebsd-amd64,amd64,freebsd,GOAMD64=v3))
$(eval $(call platform_rule,freebsd-arm64,arm64,freebsd))
$(eval $(call platform_rule,windows-386,386,windows,,.exe))
$(eval $(call platform_rule,windows-amd64-compatible,amd64,windows,GOAMD64=v1,.exe))
$(eval $(call platform_rule,windows-amd64,amd64,windows,GOAMD64=v3,.exe))
$(eval $(call platform_rule,windows-amd64-v1,amd64,windows,GOAMD64=v1,.exe))
$(eval $(call platform_rule,windows-amd64-v2,amd64,windows,GOAMD64=v2,.exe))
$(eval $(call platform_rule,windows-amd64-v3,amd64,windows,GOAMD64=v3,.exe))
$(eval $(call platform_rule,windows-arm64,arm64,windows,,.exe))
$(eval $(call platform_rule,windows-arm32v7,arm,windows,GOARM=7,.exe))

gz_releases=$(addsuffix .gz, $(PLATFORM_LIST))
zip_releases=$(addsuffix .zip, $(WINDOWS_ARCH_LIST))

$(gz_releases): %.gz : %
	chmod +x $(BINDIR)/$(NAME)-$(basename $@)
	gzip -f -S -$(VERSION).gz $(BINDIR)/$(NAME)-$(basename $@)

$(zip_releases): %.zip : %
	zip -m -j $(BINDIR)/$(NAME)-$(basename $@)-$(VERSION).zip $(BINDIR)/$(NAME)-$(basename $@).exe

all-arch: $(PLATFORM_LIST) $(WINDOWS_ARCH_LIST)

releases: $(gz_releases) $(zip_releases)

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm $(BINDIR)/*
