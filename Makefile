# oxa — top-level Makefile.
# Most targets activate once go/go.mod exists (M3). The `vectors` target
# activates at M2.

GO_MOD := go/go.mod

# Helper: run the go command in go/, or print the not-yet-available notice.
# $(1) = command to run inside go/ ; $(2) = target name ; $(3) = milestone
define go_target
	@if [ -f $(GO_MOD) ]; then \
		cd go && $(1); \
	else \
		echo "$(2): not yet available — activates at milestone $(3) (no $(GO_MOD) yet)."; \
	fi
endef

.PHONY: test vectors lint fmt check-modulepath test-python

test:
	$(call go_target,go test ./...,$@,M3)

test-python:
	@if [ -d python/src ]; then \
		cd python && PYTHONPATH=src python3 -m unittest discover -s tests; \
	else \
		echo "test-python: python/src directory not found."; \
		exit 1; \
	fi

vectors:
	$(call go_target,go run ./cmd/veccheck -root ..,$@,M2)

lint:
	$(call go_target,go vet ./...,$@,M3)

fmt:
	$(call go_target,gofmt -l .,$@,M3)

check-modulepath:
	@echo "check-modulepath: scanning Go module files for placeholder markers..."
	@found=0; \
	for f in go/go.mod go/*/go.mod; do \
		[ -f "$$f" ] || continue; \
		if grep -nE 'GH_USER|oxa-protocol' "$$f"; then found=1; fi; \
	done; \
	if [ "$$found" -eq 1 ]; then \
		echo "check-modulepath: placeholder module path found (see above)."; \
		exit 1; \
	else \
		echo "check-modulepath: none found (no Go module files yet, or clean)."; \
	fi
