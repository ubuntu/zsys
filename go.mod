module github.com/ubuntu/zsys

go 1.20

require (
	github.com/bicomsystems/go-libzfs v0.3.3
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/godbus/dbus/v5 v5.1.0
	github.com/google/go-cmp v0.5.9
	github.com/k0kubun/pp v3.0.1+incompatible
	github.com/sirupsen/logrus v1.9.3
	github.com/snapcore/go-gettext v0.0.0-20201130093759-38740d1bd3d2
	github.com/spf13/cobra v1.7.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.4
	golang.org/x/sys v0.10.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230711160842-782d3b101e98
	google.golang.org/grpc v1.56.2
	google.golang.org/protobuf v1.31.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/k0kubun/colorstring v0.0.0-20150214042306-9440f1994b88 // indirect
	github.com/mattn/go-colorable v0.1.2 // indirect
	github.com/mattn/go-isatty v0.0.8 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	golang.org/x/net v0.12.0 // indirect
	golang.org/x/text v0.11.0 // indirect
)

replace github.com/bicomsystems/go-libzfs => github.com/ubuntu/go-libzfs v0.2.2-0.20230711233110-6b487f8211c2
