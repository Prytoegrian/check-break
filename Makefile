PACKAGE = github.com/prytoegrian/check-break

# Functions

# make_version
# params : $(1) version
#
define make_version
	@semver inc $(1)
	@echo "New release: `semver tag`"
	@git add .semver
	@git commit -m "Releasing `semver tag`"
	@git tag -a `semver tag` -m "Releasing `semver tag`"
endef

default : install

build:
	go build $(PACKAGE)/...

install: build
	go install ./

major:
	$(call make_version,major)

minor:
	$(call make_version,minor)

patch:
	$(call make_version,patch)
