package testutils

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var (
	sdbus sync.Once

	sdbusMU             sync.Mutex
	nbRunningTestsSdbus uint
)

// StartLocalSystemBus allows to start and set environment variable to a local bus, preventing polluting system ones
func StartLocalSystemBus(t *testing.T) func() {
	t.Helper()

	sdbusMU.Lock()
	defer sdbusMU.Unlock()
	nbRunningTestsSdbus++

	sdbus.Do(func() {
		dir, cleanup := TempDir(t)
		savedDbusSystemAddress := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS")
		config := filepath.Join(dir, "dbus.config")
		ioutil.WriteFile(config, []byte(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>system</type>
  <keep_umask/>
  <listen>unix:tmpdir=/tmp</listen>
  <standard_system_servicedirs />
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>`), 0666)
		ctx, stopDbus := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, "dbus-daemon", "--print-address=1", "--config-file="+config)
		dbusStdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("couldn't get stdout of dbus-daemon: %v", err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("couldn't start dbus-daemon: %v", err)
		}
		dbusAddr := make([]byte, 256)
		n, err := dbusStdout.Read(dbusAddr)
		if err != nil {
			t.Fatalf("couldn't get dbus address: %v", err)
		}
		dbusAddr = dbusAddr[:n]
		if err := os.Setenv("DBUS_SYSTEM_BUS_ADDRESS", string(dbusAddr)); err != nil {
			t.Fatalf("couldn't set DBUS_SYSTEM_BUS_ADDRESS: %v", err)
		}

		go func() {
			for {
				time.Sleep(time.Second)
				sdbusMU.Lock()

				// Wait for all tests that started to be done to cleanup properly
				if nbRunningTestsSdbus != 0 {
					sdbusMU.Unlock()
					continue
				}

				stopDbus()
				cmd.Wait()

				if err := os.Setenv("DBUS_SYSTEM_BUS_ADDRESS", savedDbusSystemAddress); err != nil {
					t.Errorf("couldn't restore DBUS_SYSTEM_BUS_ADDRESS: %v", err)
				}
				cleanup()

				// Restore dbus system launcher
				sdbus = sync.Once{}
				sdbusMU.Unlock()
				break
			}
		}()
	})

	return func() {
		sdbusMU.Lock()
		defer sdbusMU.Unlock()
		nbRunningTestsSdbus--
	}
}
