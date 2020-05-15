
GO ?= go
#GO = /a/opt/go/bin/go
OUT ?= .

-include ${HOME}/.dmeshrc
# ARM/MIPS binaries - about 11MB
#

all:
	GOARCH=mips GOOS=linux GOMIPS=softfloat ${GO} build -ldflags="-s -w" -o ${OUT}/bin/mips/dml2 .
	GOARCH=arm GOOS=linux GOARM=7 ${GO} build -ldflags="-s -w" -o ${OUT}/bin/arm/dml2 .
	GOARCH=arm64 GOOS=linux GOARM=7 ${GO} build -ldflags="-s -w" -o ${OUT}/bin/arm64/dml2 .
	${GO} build -ldflags="-s -w" -o ${OUT}/bin/arm/dml2 .

gen:
	#(cd proto; protoc --gogo_out=plugins=grpc:./ l2.proto)
	(cd pkg/l2api; PATH=${GOPATH}/bin:${PATH} protoc --gogo_out=./ l2.proto)

gogo:
	go get github.com/gogo/protobuf/proto
	go get github.com/gogo/protobuf/jsonpb
	go get github.com/gogo/protobuf/gogoproto
	go get github.com/gogo/protobuf/protoc-gen-gogo
	go get github.com/gogo/protobuf/protoc-gen-gogofast
	go get github.com/gogo/protobuf/protoc-gen-gogoslick
