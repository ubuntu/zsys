# zsys
zsys daemon and client for zfs systems

[![Code quality](https://github.com/ubuntu/zsys/workflows/CI/badge.svg)](https://github.com/ubuntu/zsys/actions?query=workflow%3ACI)
[![Go Report Card](https://goreportcard.com/badge/ubuntu/zsys)](https://goreportcard.com/report/ubuntu/zsys)
[![codecov](https://codecov.io/gh/ubuntu/zsys/branch/master/graph/badge.svg)](https://codecov.io/gh/ubuntu/zsys)
[![License](https://img.shields.io/badge/License-GPL3.0-blue.svg)](https://github.com/ubuntu/zsys/blob/master/LICENSE)

ZSYS is a Zfs SYStem tool targeting an enhanced ZOL experience.

It allows running multiple ZFS systems in parallel on the same machine, get automated snapshots, managing complex zfs dataset layouts separating user data from system and persistent data, and more.

## Usage

### User commands

#### zsysctl

ZFS SYStem integration control zsys daemon

##### Synopsis

Zfs SYStem tool for an enhanced ZFS on Linux experience.
 It allows running multiple ZFS system in parallels on the same machine,
 get automated snapshots, managing complex zfs dataset layouts separating
 user data from system and persistent data, and more.

```
zsysctl COMMAND [flags]
```

##### Options

```
  -h, --help            help for zsysctl
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl completion

Generates bash completion scripts

##### Synopsis

To load completion run

. <(zsysctl completion)

To configure your bash shell to load completions for each session add to your ~/.bashrc or ~/.profile:

. <(zsysctl completion)


```
zsysctl completion [flags]
```

##### Options

```
  -h, --help   help for completion
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl list

List all the machines and basic information.

##### Synopsis

Alias of zsysctl machine list

```
zsysctl list [flags]
```

##### Options

```
  -h, --help   help for list
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl machine

Machine management

##### Synopsis

Machine management

```
zsysctl machine COMMAND [flags]
```

##### Options

```
  -h, --help   help for machine
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl machine list

List all the machines and basic information.

##### Synopsis

List all the machines and basic information.

```
zsysctl machine list [flags]
```

##### Options

```
  -h, --help   help for list
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl machine show

Shows the status of the machine.

##### Synopsis

Shows the status of the machine.

```
zsysctl machine show [MachineID] [flags]
```

##### Options

```
      --full   Give more detail informations on each machine.
  -h, --help   help for show
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl save

Saves the current state of the machine. By default it saves only the user state. state_id is generated if not provided.

##### Synopsis

Alias of zsysctl state save

```
zsysctl save [state id] [flags]
```

##### Options

```
  -h, --help                 help for save
      --no-update-bootmenu   Do not update bootmenu on system state save
  -s, --system               Save complete system state (users and system)
  -u, --user string          Save the state for a given user or current user if empty
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service

Service management

##### Synopsis

Service management

```
zsysctl service COMMAND [flags]
```

##### Options

```
  -h, --help   help for service
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service dump

Dumps the current state of zsys.

##### Synopsis

Dumps the current state of zsys.

```
zsysctl service dump [flags]
```

##### Options

```
  -h, --help   help for dump
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service gc

Run daemon state saves garbage collection.

##### Synopsis

Run daemon state saves garbage collection.

```
zsysctl service gc [flags]
```

##### Options

```
  -a, --all    Collects all the datasets including manual snapshots and clones.
  -h, --help   help for gc
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service loglevel

Sets the logging level of the daemon.

##### Synopsis

Sets the logging level of the daemon.

```
zsysctl service loglevel 0|1|2 [flags]
```

##### Options

```
  -h, --help   help for loglevel
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service refresh

Refreshes machines states.

##### Synopsis

Refreshes machines states.

```
zsysctl service refresh [flags]
```

##### Options

```
  -h, --help   help for refresh
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service reload

Reloads daemon configuration.

##### Synopsis

Reloads daemon configuration.

```
zsysctl service reload [flags]
```

##### Options

```
  -h, --help   help for reload
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service status

Shows the status of the daemon.

##### Synopsis

Shows the status of the daemon.

```
zsysctl service status [flags]
```

##### Options

```
  -h, --help   help for status
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service stop

Stops zsys daemon.

##### Synopsis

Stops zsys daemon.

```
zsysctl service stop [flags]
```

##### Options

```
  -h, --help   help for stop
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl service trace

Start profiling until you exit this command yourself or when duration is done. Default is CPU profiling with a 30s timeout.

##### Synopsis

Start profiling until you exit this command yourself or when duration is done. Default is CPU profiling with a 30s timeout.

```
zsysctl service trace [flags]
```

##### Options

```
      --duration int    Duration of the capture. Default is 30 seconds. (default 30)
  -h, --help            help for trace
  -o, --output string   Dump the trace to a file. Default is ./zsys.<trace-type>.pprof
  -t, --type string     Type of profiling cpu or mem. Default is cpu. (default "cpu")
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl show

Shows the status of the machine.

##### Synopsis

Alias of zsysctl machine show

```
zsysctl show [MachineID] [flags]
```

##### Options

```
      --full   Give more detail informations on each machine.
  -h, --help   help for show
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl state

Machine state management

##### Synopsis

Machine state management

```
zsysctl state COMMAND [flags]
```

##### Options

```
  -h, --help   help for state
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl state remove

Remove the current state of the machine. By default it removes only the user state if not linked to any system state.

##### Synopsis

Remove the current state of the machine. By default it removes only the user state if not linked to any system state.

```
zsysctl state remove [state id] [flags]
```

##### Options

```
  -f, --force         Force removing, even if dependencies are found
  -h, --help          help for remove
  -s, --system        Remove system state (system and users linked to it)
  -u, --user string   Remove the state for a given user or current user if empty
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl state save

Saves the current state of the machine. By default it saves only the user state. state_id is generated if not provided.

##### Synopsis

Saves the current state of the machine. By default it saves only the user state. state_id is generated if not provided.

```
zsysctl state save [state id] [flags]
```

##### Options

```
  -h, --help                 help for save
      --no-update-bootmenu   Do not update bootmenu on system state save
  -s, --system               Save complete system state (users and system)
  -u, --user string          Save the state for a given user or current user if empty
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl version

Returns version of client and server

##### Synopsis

Returns version of client and server

```
zsysctl version [flags]
```

##### Options

```
  -h, --help   help for version
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysd

ZFS SYStem integration daemon

##### Synopsis

Zfs SYStem daemon for an enhanced ZFS on Linux experience.
 It allows running multiple ZFS system in parallels on the same machine,
 get automated snapshots, managing complex zfs dataset layouts separating
 user data from system and persistent data, and more.

```
zsysd [flags]
```

##### Options

```
  -h, --help            help for zsysd
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysd completion

Generates bash completion scripts

##### Synopsis

To load completion run

. <(zsysd completion)

To configure your bash shell to load completions for each session add to your ~/.bashrc or ~/.profile:

. <(zsysd completion)


```
zsysd completion [flags]
```

##### Options

```
  -h, --help   help for completion
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

### System commands

Those commands are hidden from help and should primarily be used by the system itself.

#### zsysctl boot

Ensure that the right datasets are ready to be mounted and committed during early boot

##### Synopsis

Ensure that the right datasets are ready to be mounted and committed during early boot

```
zsysctl boot COMMAND [flags]
```

##### Options

```
  -h, --help            help for boot
  -p, --print-changes   Display if any zfs datasets have been modified to boot
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl boot commit

Commit system and user datasets states as a successful boot

##### Synopsis

Commit system and user datasets states as a successful boot

```
zsysctl boot commit [flags]
```

##### Options

```
  -h, --help   help for commit
```

##### Options inherited from parent commands

```
  -p, --print-changes   Display if any zfs datasets have been modified to boot
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl boot prepare

Prepare boot by ensuring correct system and user datasets are switched on and off

##### Synopsis

Prepare boot by ensuring correct system and user datasets are switched on and off

```
zsysctl boot prepare [flags]
```

##### Options

```
  -h, --help   help for prepare
```

##### Options inherited from parent commands

```
  -p, --print-changes   Display if any zfs datasets have been modified to boot
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl userdata

User datasets creation and rename

##### Synopsis

User datasets creation and rename

```
zsysctl userdata COMMAND [flags]
```

##### Options

```
  -h, --help   help for userdata
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl userdata create

Create a new home user dataset via an user dataset (if doesn't exist) creation

##### Synopsis

Create a new home user dataset via an user dataset (if doesn't exist) creation

```
zsysctl userdata create USER HOME_DIRECTORY [flags]
```

##### Options

```
  -h, --help   help for create
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysctl userdata set-home

Rename a user's home directory via renaming corresponding user dataset

##### Synopsis

Rename a user's home directory via renaming corresponding user dataset

```
zsysctl userdata set-home OLD_HOME NEW_HOME [flags]
```

##### Options

```
  -h, --help   help for set-home
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

#### zsysd boot-prepare

Prepare boot by ensuring correct system and user datasets are switched on and off, synchronously

##### Synopsis

Prepare boot by ensuring correct system and user datasets are switched on and off, synchronously

```
zsysd boot-prepare [flags]
```

##### Options

```
  -h, --help   help for boot-prepare
```

##### Options inherited from parent commands

```
  -v, --verbose count   issue INFO (-v) and DEBUG (-vv) output
```

