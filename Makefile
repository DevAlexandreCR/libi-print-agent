.PHONY: tidy vet test build-windows vet-windows clean

tidy:
	./scripts/build.sh tidy

vet:
	./scripts/build.sh vet

test:
	./scripts/build.sh test

build-windows:
	./scripts/build.sh build-windows

vet-windows:
	./scripts/build.sh vet-windows

clean:
	rm -rf dist
