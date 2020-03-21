
#GO ?= go
GO = /a/opt/go/bin/go
OUT ?= .

all:
	GOARCH=mips GOOS=linux GOMIPS=softfloat ${GO} build -ldflags="-s -w" -o ${OUT}/bin/mips/dml2 .
	GOARCH=arm GOOS=linux GOARM=7 ${GO} build -ldflags="-s -w" -o ${OUT}/bin/arm/dml2 .
	GOARCH=arm64 GOOS=linux GOARM=7 ${GO} build -ldflags="-s -w" -o ${OUT}/bin/arm64/dml2 .
	${GO} build -ldflags="-s -w" -o ${OUT}/bin/arm/dml2 .
