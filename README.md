# perf-load

Augments perf samples with number of CPUs running at the time of sample.

Usage:
```
$ perf record -a -g -e cycles -e 'sched:sched_switch' ./binary
$ perf-load -o /tmp/pprof
```
