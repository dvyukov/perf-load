If you are into profiling and optimization, I have a simple question for you:
what do traditional profilers (like
[perf](https://perf.wiki.kernel.org/index.php/Tutorial) and
[pprof](https://github.com/google/pprof)) sample: real time or CPU time?
And what are implications?

In the old single-core era these were the same (almost), and we used to just
look at the top entries and optimize them. But in the parallel world, these
are significantly different. If you are interested in CPU usage reduction,
then you need to look at a profile that sampled CPU time (perf, pprof).
However, frequently we are more interested in real execution time/latency
reduction (HPC, browser page rendering or compute-intensive request serving).
And in this case you need a profile that samples real time. Here is a simple
example: we have 2 seconds of single-threaded computations followed by 64
seconds of parallel computations. A traditional CPU-sampling profile will
show you that 97% of the time is spend in the parallel computations,
so that's what you will concentrate on. However, on a 64-core machine the
parallel computations take only 1 second, of half of the sequential part.
So any optimization effort on the single-threaded part is twice as profitable
as optimization of the parallel part. Even if single-threaded code takes less
real time then parallel code, it still may be worth optimizing. Just because
it is simpler to optimize and it is probably under-optimized with lots of
low-hanging fruit (we were looking at CPU time profile and concentrated on
the parallel part before). But we need data!

I did not find any profilers that can sample real time (or convert CPU time
to real time during presentation). So I hacked up this prototype.

It takes a specially-collected perf profile (profile needs to include context
switch events), augments each sample with number of CPUs executing threads of
the process during the sample and writes out pprof profile (not sure if it is
possible to feed a modified profile back into perf).

Here is real usage example for a [TensorFlow](https://www.tensorflow.org/)-based
program. It includes a bunch of optimized parallel kernels for computations
like matrix multiplication, these kernels are glued together by
single-threaded Python. Python is slow, but who cares if it just invokes
highly-optimized C, right? It was
[reported](https://github.com/tensorflow/tensorflow/issues/583#issuecomment-204665155)
that Tensorflow computations scale poorly above ~16 cores
(and that's what I observe locally as well).

If you look at a normal perf/pprof profile, you will see
(Eigen is linear algebra C++ library behind TensorFlow):
```
      flat  flat%   sum%        cum   cum%
26058.25ms 18.83% 18.83% 26663.75ms 19.27%  Eigen::...
25886.50ms 18.71% 37.54% 25982.75ms 18.78%  Eigen::...
   24525ms 17.72% 55.26% 24603.50ms 17.78%  Eigen::...
11354.50ms  8.21% 63.46% 11391.75ms  8.23%  Eigen::...
10196.75ms  7.37% 70.83% 10237.75ms  7.40%  Eigen::...
    7037ms  5.09% 75.92%  7068.25ms  5.11%  Eigen::...
 4307.75ms  3.11% 79.03%     4331ms  3.13%  Eigen::...
    3496ms  2.53% 81.56%     3645ms  2.63%  Eigen::...
 3293.50ms  2.38% 83.94%     3308ms  2.39%  Eigen::...
    2938ms  2.12% 86.06%     2947ms  2.13%  Eigen::...
 1946.25ms  1.41% 87.47%     1949ms  1.41%  Eigen::...
 1258.25ms  0.91% 89.76%  1262.75ms  0.91%  Eigen::...
     995ms  0.72% 90.48%      998ms  0.72%  Eigen::...
  958.75ms  0.69% 91.17%  5135.50ms  3.71%  PyEval_EvalFrameEx
...
```
Looks good: Eigen is highly optimized and parallelized C++, so there is
nothing more we can do... or?

The first thing that perf-load tool can provide is a histogram of CPU load:
```
 1 [ 3.77%]: ***********************************************************************************
 2 [ 0.29%]: ******
 3 [ 0.31%]: *******
 4 [ 0.41%]: *********
 5 [ 0.50%]: ***********
 6 [ 1.17%]: **************************
 7 [ 0.62%]: **************
 8 [ 3.54%]: ******************************************************************************
 9 [ 1.06%]: ***********************
10 [ 1.04%]: ***********************
11 [ 1.20%]: **************************
12 [ 1.22%]: ***************************
13 [ 1.26%]: ****************************
14 [ 1.31%]: *****************************
15 [ 1.40%]: *******************************
16 [ 1.79%]: ***************************************
17 [ 1.33%]: *****************************
18 [ 1.45%]: ********************************
19 [ 1.51%]: *********************************
20 [ 1.97%]: *******************************************
21 [ 2.07%]: **********************************************
22 [ 2.11%]: ***********************************************
23 [ 2.35%]: ****************************************************
24 [ 2.58%]: *********************************************************
25 [ 2.89%]: ****************************************************************
26 [ 2.87%]: ***************************************************************
27 [ 3.02%]: *******************************************************************
28 [ 3.47%]: *****************************************************************************
29 [ 3.50%]: *****************************************************************************
30 [ 3.56%]: *******************************************************************************
31 [ 4.06%]: ******************************************************************************************
32 [ 3.85%]: *************************************************************************************
33 [ 4.49%]: ***************************************************************************************************
34 [ 4.30%]: ***********************************************************************************************
35 [ 4.39%]: *************************************************************************************************
36 [ 4.53%]: ****************************************************************************************************
37 [ 3.81%]: ************************************************************************************
38 [ 3.61%]: ********************************************************************************
39 [ 3.25%]: ************************************************************************
40 [ 2.22%]: *************************************************
41 [ 2.07%]: **********************************************
42 [ 1.41%]: *******************************
43 [ 1.35%]: ******************************
44 [ 0.49%]: ***********
45 [ 0.24%]: *****
46 [ 0.10%]: **
47 [ 0.07%]: **
```
We can see that we load 1 CPU only 3.77% of time. This allows us to estimate
quality of our parallelization. We can also see that parallel computations
don't load all cores most of the time.

But that was still in CPU time. Now we can rescale the histogram to real time
(namely, a sample obtained when X threads were running is X times less valuable
that a sample obtained when only 1 thread was running):
```
 1 [46.00%]: ****************************************************************************************************
 2 [ 1.71%]: ****
 3 [ 1.19%]: ***
 4 [ 1.17%]: ***
 5 [ 1.16%]: ***
 6 [ 2.25%]: *****
 7 [ 1.01%]: **
 8 [ 5.16%]: ***********
 9 [ 1.37%]: ***
10 [ 1.20%]: ***
11 [ 1.26%]: ***
12 [ 1.17%]: ***
13 [ 1.11%]: **
14 [ 1.08%]: **
15 [ 1.09%]: **
16 [ 1.28%]: ***
17 [ 0.89%]: **
18 [ 0.92%]: **
19 [ 0.91%]: **
20 [ 1.13%]: **
21 [ 1.13%]: **
22 [ 1.11%]: **
23 [ 1.19%]: ***
24 [ 1.22%]: ***
25 [ 1.32%]: ***
26 [ 1.26%]: ***
27 [ 1.28%]: ***
28 [ 1.43%]: ***
29 [ 1.38%]: ***
30 [ 1.36%]: ***
31 [ 1.51%]: ***
32 [ 1.38%]: ***
33 [ 1.57%]: ***
34 [ 1.46%]: ***
35 [ 1.45%]: ***
36 [ 1.45%]: ***
37 [ 1.19%]: ***
38 [ 1.09%]: **
39 [ 0.96%]: **
40 [ 0.64%]: *
41 [ 0.58%]: *
42 [ 0.39%]: *
43 [ 0.36%]: *
44 [ 0.13%]: 
45 [ 0.06%]: 
46 [ 0.03%]: 
47 [ 0.02%]:
```
Ouch! Now it turns out that we load only 1 CPU 46% of [real] time.
And parallelization looks even worse (and this reflects the real situation).

Now we can select only samples that were obtained when 1 thread was running:
```
      flat  flat%   sum%        cum   cum%
    44.88s 17.55% 17.55%    240.68s 94.09%  PyEval_EvalFrameEx
     8.86s  3.46% 21.01%      9.39s  3.67%  _PyType_Lookup
     8.64s  3.38% 24.39%     30.36s 11.87%  _PyObject_GenericGetAttrWithDict
     8.39s  3.28% 27.67%      8.71s  3.40%  lookdict_string
     6.82s  2.66% 30.33%     19.53s  7.63%  collect
     5.35s  2.09% 32.42%      7.41s  2.90%  PyParser_AddToken
     4.59s  1.80% 34.22%    240.65s 94.08%  PyEval_EvalCodeEx
     4.58s  1.79% 36.01%      4.88s  1.91%  PyObject_Malloc
     4.03s  1.58% 37.58%      4.92s  1.92%  PyFrame_New
     3.47s  1.36% 38.94%      3.49s  1.36%  visit_decref
     3.23s  1.26% 40.20%      3.23s  1.26%  PyObject_GetAttr
     3.21s  1.25% 41.46%      3.21s  1.25%  copy_user_enhanced_fast_string
     3.16s  1.24% 42.69%      6.17s  2.41%  frame_dealloc
     3.06s  1.19% 43.89%      3.17s  1.24%  tupledealloc
     2.73s  1.07% 44.95%      2.80s  1.09%  dict_traverse
     2.62s  1.02% 45.98%      2.63s  1.03%  visit_reachable
     2.28s  0.89% 46.87%      2.30s   0.9%  PyObject_Free
     2.19s  0.85% 47.72%      5.88s  2.30%  PyDict_GetItem
     1.89s  0.74% 48.46%      8.95s  3.50%  PyTuple_New.part.3
     1.70s  0.67% 49.13%      5.05s  1.98%  page_fault
     1.55s  0.61% 49.73%      1.59s  0.62%  Eigen::...
```
Here you are, Python!

I need to note that it's not always that bad. Time in Python depends on size
of matrices that we process, for larger matrices more time spent in Eigen.
But still I see 10-30% of time in Python on what I've been told to be
realistic matrix sizes.

This data lead to the
[following "one-line" fix](https://github.com/soumith/convnet-benchmarks/commit/605988f847fcf261c288abc2351597d71a2ee149)
in the way the benchmark was written. The issue was subtle enough that many
sets of eyes on similar pieces of code hadn't spotted it, even in the main
set of public benchmarks that people are using to compare the performance of
TensorFlow, Torch, Caffe, Theano, etc. The fix reduced AlexNext/Googlenet
end-to-end latency by 10%.

The tool also easily identified lots of unparallelized TensorFlow operations
that were significantly affecting end-to-end latency. Functions that were
consuming <1% in CPU profiles and were ignored suddenly jumped to 10-30%
in real time profiles.

The tool can also select, say, only samples when 2-8 CPUs running. And then
you can compare that to profile for 32-48 CPUs. It turns out there are some
differences. For example, you can see that one algorithm is better parallelized
than another one, or you can observe different contention levels.

Here is another hypothetical use case. Let's say we are lookig at a
traditional CPU profile of a Go program and quite some time is spent in the
garbage collector. But that's only CPU time. We don't know what happens there
with respect to real time. We can imagine several possibilities. If there are
large single-threaded stop-the-world GC phases, then we undersample GC with
respect to real time and its effect on latency is even worse. Or, on the other
hand, if GC is fully concurrent and parallel and just increases CPU load
during collection, then we oversample GC with respect to real time and its
effect is not that bad.

Here is another interesting view of the profile: we can calculate for each
function average CPU load when we hit this function. For example:
```
  10% parallel_algorithm1 [avgload=30]
  10% parallel_algorithm2 [avgload=10]
```
Let's assume the percents are already scaled to real time here. Such data may
suggest that parallel_algorithm2 is poorly parallelized, and if we achieve
the same avgload=30 for it, we will reduce total execution time by 33%.

Usage instructions (may be outdated):
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
