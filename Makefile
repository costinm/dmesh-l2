
GO ?= go build -ldflags="-s -w"
#GO = /a/opt/go/bin/go
OUT ?= .

-include ${HOME}/.dmeshrc
# ARM/MIPS binaries - about 11MB
#
all: libDM dml2

libDM:
	GOARCH=arm64 GOOS=linux GOARM=7 ${GO} -o ${OUT}/bin/arm64/libDM.so ./cmd/libDM
	GOARCH=mips GOOS=linux GOMIPS=softfloat ${GO}  -o ${OUT}/bin/mips/libDM.so ./cmd/libDM
	GOARCH=arm GOOS=linux GOARM=7 ${GO}  -o ${OUT}/bin/arm/libDM.so ./cmd/libDM
	${GO} -o ${OUT}/bin/libDM.so ./cmd/libDM

dml2:
	GOARCH=arm64 GOOS=linux GOARM=7 ${GO} -o ${OUT}/bin/arm64/dml2 ./cmd/dmesh-min
	GOARCH=mips GOOS=linux GOMIPS=softfloat ${GO} -o ${OUT}/bin/mips/dml2 ./cmd/dmesh-min
	GOARCH=arm GOOS=linux GOARM=7 ${GO} -o ${OUT}/bin/arm/dml2 ./cmd/dmesh-min
	${GO} -o ${OUT}/bin/dml2 ./cmd/dmesh-min


