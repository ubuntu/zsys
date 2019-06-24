module github.com/ubuntu/zsys

go 1.12

require (
	github.com/bicomsystems/go-libzfs v0.2.3
	github.com/google/go-cmp v0.3.0
	golang.org/x/xerrors v0.0.0-20190513163551-3ee3066db522
	gopkg.in/yaml.v2 v2.2.2
)

// We remove rlim64_t duplicated definition: https://github.com/bicomsystems/go-libzfs/pull/17
//replace github.com/bicomsystems/go-libzfs => github.com/ubuntu/go-libzfs v0.0.0-20190606120954-6db09288f0f1
