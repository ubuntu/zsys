module github.com/ubuntu/zsys

go 1.13

require (
	github.com/bicomsystems/go-libzfs v0.3.3
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f
	github.com/godbus/dbus/v5 v5.0.3
	github.com/golang/protobuf v1.4.3
	github.com/google/go-cmp v0.4.0
	github.com/k0kubun/colorstring v0.0.0-20150214042306-9440f1994b88 // indirect
	github.com/k0kubun/pp v3.0.1+incompatible
	github.com/mattn/go-colorable v0.1.2 // indirect
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd
	github.com/sirupsen/logrus v1.4.2
	github.com/snapcore/go-gettext v0.0.0-20190812090936-a77afd68d2bd
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.3
	github.com/stretchr/testify v1.2.2
	golang.org/x/net v0.0.0-20191002035440-2ec189313ef0 // indirect
	golang.org/x/sys v0.0.0-20191002091554-b397fe3ad8ed
	golang.org/x/text v0.3.2 // indirect
	google.golang.org/genproto v0.0.0-20200309141739-5b75447e413d
	google.golang.org/grpc v1.27.0
	google.golang.org/protobuf v1.23.0
	gopkg.in/yaml.v2 v2.2.2
)

replace github.com/bicomsystems/go-libzfs => github.com/ubuntu/go-libzfs v0.2.2-0.20220406085817-43edd0b6397a
