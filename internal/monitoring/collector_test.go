package monitoring

import (
	"testing"
)

func TestParseCollectionOutput(t *testing.T) {
	rawSample := `===SYS===
Linux 5.15.0-89-generic x86_64
up 12 days, 4 hours, 22 minutes
0.45 0.32 0.18 1/450 98765
prod-app-01
===CPU===
4
cpu  45678 1234 12345 567890 1200 0 450 0 0 0
===MEM===
              total        used        free      shared  buff/cache   available
Mem:    16777216000  8388608000  4194304000   104857600  4194304000  7864320000
Swap:    4294967296  1073741824  3221225472
===DISK===
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/sda1        104857600  41943040  62914560      40% /
/dev/sdb1        209715200  83886080 125829120      40% /data
tmpfs              8388608         0   8388608       0% /dev/shm
===NET===
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 12345678      10    0    0    0     0          0         0 12345678      10    0    0    0     0       0          0
  eth0: 987654321    100    0    0    0     0          0         0 123456789    100    0    0    0     0       0          0
===PROC===
  PID USER      %CPU %MEM COMMAND
 1234 root      25.4  4.2 /usr/bin/dockerd
 5678 www-data  12.1  2.1 nginx: worker process
  999 postgres   5.2  8.0 postgres: main
===SVC===
ssh:active
nginx:active
docker:active
redis:inactive
`

	m := ParseCollectionOutput(rawSample)

	if m.OSInfo.OSName != "Linux" || m.OSInfo.Kernel != "5.15.0-89-generic" || m.OSInfo.Arch != "x86_64" {
		t.Fatalf("unexpected OSInfo: %+v", m.OSInfo)
	}
	if m.CPU.Cores != 4 {
		t.Fatalf("cores = %d, want 4", m.CPU.Cores)
	}
	if m.CPU.Load1 != 0.45 || m.CPU.Load5 != 0.32 || m.CPU.Load15 != 0.18 {
		t.Fatalf("unexpected loads: %v, %v, %v", m.CPU.Load1, m.CPU.Load5, m.CPU.Load15)
	}
	if m.Memory.TotalBytes != 16777216000 || m.Memory.UsedBytes != 8388608000 {
		t.Fatalf("unexpected memory: %+v", m.Memory)
	}
	if m.Memory.UsagePercent != 50.0 {
		t.Fatalf("memory usage percent = %v, want 50.0", m.Memory.UsagePercent)
	}
	if len(m.Disks) != 2 {
		t.Fatalf("disk count = %d, want 2", len(m.Disks))
	}
	if m.Disks[0].MountPoint != "/" || m.Disks[1].MountPoint != "/data" {
		t.Fatalf("unexpected disk mount points: %+v", m.Disks)
	}
	if len(m.Processes) != 3 {
		t.Fatalf("processes count = %d, want 3", len(m.Processes))
	}
	if m.Processes[0].PID != 1234 || m.Processes[0].CPU != 25.4 {
		t.Fatalf("unexpected top process: %+v", m.Processes[0])
	}
	if len(m.Services) != 4 {
		t.Fatalf("services count = %d, want 4", len(m.Services))
	}
	if m.Services[0].Name != "ssh" || m.Services[0].Status != "active" {
		t.Fatalf("unexpected service: %+v", m.Services[0])
	}
}
