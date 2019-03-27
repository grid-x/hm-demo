
all: bin/hm-demo bin/hm-demo-arm.linux

bin: 
	mkdir -p bin
now: bin
	CGO_ENABLED=0 go build -o bin/hm-demo ./cmd/hm-demo

bin/hm-demo: bin
	go build -o bin/hm-demo ./cmd/hm-demo

bin/hm-demo-arm.linux: bin
	GOOS=linux GOARCH=arm go build -o bin/hm-demo-arm.linux ./cmd/hm-demo

.PHONY: clean
clean:
	rm -f ./bin/*
