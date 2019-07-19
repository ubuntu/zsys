module github.com/ubuntu/zsys

go 1.12

require (
	github.com/bicomsystems/go-libzfs v0.2.3
	github.com/google/go-cmp v0.3.0
	github.com/k0kubun/colorstring v0.0.0-20150214042306-9440f1994b88 // indirect
	github.com/k0kubun/pp v3.0.1+incompatible
	github.com/mattn/go-colorable v0.1.2 // indirect
	github.com/sirupsen/logrus v1.4.2
	golang.org/x/sys v0.0.0-20190606165138-5da285871e9c // indirect
	golang.org/x/xerrors v0.0.0-20190717185122-a985d3407aa7
	gopkg.in/yaml.v2 v2.2.2
)

// Check https://github.com/bicomsystems/go-libzfs/pull/20
replace github.com/bicomsystems/go-libzfs => github.com/ubuntu/go-libzfs v0.2.2-0.20190715120733-dfe7058c3c21
