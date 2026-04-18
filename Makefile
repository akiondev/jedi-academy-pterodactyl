.PHONY: test-go

GO ?= go
GO_TEST_CACHE ?= /tmp/jedi-academy-pterodactyl-go-build-cache
GO_TEST_MODCACHE ?= /tmp/jedi-academy-pterodactyl-go-mod-cache

test-go:
	GOCACHE="$(GO_TEST_CACHE)" GOMODCACHE="$(GO_TEST_MODCACHE)" $(GO) test ./...
