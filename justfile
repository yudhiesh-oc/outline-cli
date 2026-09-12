set dotenv-load := false

build_dir := "bin"
artifact := build_dir + "/outline"
default_install_dir := env_var("HOME") + "/.local/bin"
install_dir := env_var_or_default("OUTLINE_BIN_DIR", default_install_dir)
binary := install_dir + "/outline"

# Run the default local checks.
default: check

check: format test vet

format:
    @files=$(gofmt -l .); if [ -n "$files" ]; then printf '%s\n' "$files"; exit 1; fi

test:
    go test ./...

vet:
    go vet ./...

# Show the most complex production functions.
complexity:
    go run github.com/fzipp/gocyclo/cmd/gocyclo@latest -ignore _test.go -top 20 .

build:
    mkdir -p "{{build_dir}}"
    go build -o "{{artifact}}" ./cmd/outline

install: build
    mkdir -p "{{install_dir}}"
    install -m 755 "{{artifact}}" "{{binary}}"
    @echo "Installed {{binary}}"
