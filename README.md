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

Zfs SYStem tool targetting an enhanced ZOL experience.
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

Zfs SYStem daemon targetting an enhanced ZOL experience.
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

Those commands are hidden from help and should primarly be used by the system itself.

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

