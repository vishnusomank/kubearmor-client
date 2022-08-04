# karmor

**karmor** is a client tool to help manage [KubeArmor](github.com/kubearmor/KubeArmor).

## Installation

```
curl -sfL http://get.kubearmor.io/ | sudo sh -s -- -b /usr/local/bin
```

### Installing from Source 

Build karmor from source if you want to test the latest (pre-release) karmor version.

```
sudo apt-get install libbtrfs-dev -y
git clone https://github.com/kubearmor/kubearmor-client.git
cd kubearmor-client
make install
```

## Usage

```
CLI Utility to help manage KubeArmor

KubeArmor is a container-aware runtime security enforcement system that
restricts the behavior (such as process execution, file access, and networking
operation) of containers at the system level.

Usage:
  karmor [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  discover    Discover applicable policies
  get         Display specified resources
  help        Help about any command
  insight     Policy insight from discovery engine
  install     Install KubeArmor in a Kubernetes Cluster
  log         Observe Logs from KubeArmor
  rotate-tls  Rotate webhook controller tls certificates
  selfupdate  selfupdate this cli tool
  sysdump     Collect system dump information for troubleshooting and error report
  uninstall   Uninstall KubeArmor from a Kubernetes Cluster
  version     Display version information
  vm          VM commands for kvmservice

Flags:
  -h, --help   help for karmor

Use "karmor [command] --help" for more information about a command.
```
