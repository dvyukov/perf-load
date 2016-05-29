# perf-load

Augments perf samples with number of CPUs running at the time of sample.

Usage:
```
$ perf record -a -g -e cycles -e 'sched:sched_switch' ./binary
$ perf-load -o /tmp/pprof
```

If perf crashes during perf-load invocation, try to build tip perf: 
```
$ sudo apt-get install libunwind8-dev binutils-dev libdw-dev libelf-dev
$ git clone git://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git
$ cd linux/tools/perf
$ make
```
Then add `linux/tools/perf` to `PATH`.

To profile without sudo:

```
$ sudo sh -c 'echo 0 >/proc/sys/kernel/perf_event_paranoid'
$ sudo chmod a+rwx -R /sys/kernel/debug
```
