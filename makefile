BINARY    := claude-statusline
INSTALL   := $(HOME)/.claude/$(BINARY)

.PHONY: build fmt vet install test clean

build:
	go build -ldflags="-s -w" -o $(BINARY) .

fmt:
	go fmt ./...

vet:
	go vet ./...

install: build
	mkdir -p $(HOME)/.claude
	cp $(BINARY) $(INSTALL)
	chmod +x $(INSTALL)
	@echo ""
	@echo "Installed to $(INSTALL)"
	@echo ""
	@echo "Add this to ~/.claude/settings.json:"
	@echo '  {'
	@echo '    "statusLine": {'
	@echo '      "type": "command",'
	@echo '      "command": "$(INSTALL)"'
	@echo '    }'
	@echo '  }'

test: build
	go test -v -count=1 -cover ./...
	./test.sh

clean:
	rm -f $(BINARY)
	rm -f cover.out
