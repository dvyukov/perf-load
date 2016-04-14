package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/google/pprof/profile"
)

var (
	flagInput    = flag.String("i", "perf.data", "input perf file")
	flagOutput   = flag.String("o", "", "output perf file")
	flagPid      = flag.Int("p", 0, "target pid")
	flagRealtime = flag.Bool("realtime", true, "scale samples to real time")
)

type Proc struct {
	pid     int
	n       int
	run     int
	load    map[int]int
	samples map[uint64]*Sample
}

type Sample struct {
	n     int
	run   int
	stack *Stack
}

type Stack struct {
	frames []*profile.Location
}

type Frame struct {
	pc uint64
	fn string
}

func main() {
	flag.Parse()

	f, err := os.Create(*flagOutput)
	if err != nil {
		failf("failed to open output file: %v", err)
	}
	defer f.Close()

	perf := exec.Command("perf", "script", "-i", *flagInput, "-f", "pid,tid,cpu,event,trace,ip,sym")
	perfOut, err := perf.StdoutPipe()
	if err != nil {
		failf("failed to pipe perf output: %v", err)
	}
	procs := make(map[int]*Proc)
	done := make(chan error)
	go func() {
		tids := make(map[uint64]uint64)
		stacks := make(map[uint64]*Stack)
		locs := make(map[uint64]*profile.Location)
		funcs := make(map[string]*profile.Function)
		s := bufio.NewScanner(perfOut)
		getProc := func(pid int) *Proc {
			p := procs[pid]
			if p == nil {
				p = &Proc{
					pid:     pid,
					load:    make(map[int]int),
					samples: make(map[uint64]*Sample),
				}
				procs[pid] = p
			}
			return p
		}
		for s.Scan() {
			ln := s.Text()
			if ln == "" || ln[0] == '#' {
				continue
			}
			if strings.Contains(ln, " sched:sched_switch:") {
				i := 0
				for ; ln[i] < '0' || ln[i] > '9'; i++ {
				}
				pidPos := i
				for ; ln[i] >= '0' && ln[i] <= '9'; i++ {
				}
				pid, err := strconv.ParseUint(ln[pidPos:i], 10, 32)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to parse pid 1: %v\n", ln)
					continue
				}
				if ln[i] != '/' {
					fmt.Fprintf(os.Stderr, "failed to parse pid 2: %v\n", ln)
					continue
				}
				i++
				tidPos := i
				for ; ln[i] >= '0' && ln[i] <= '9'; i++ {
				}
				tid, err := strconv.ParseUint(ln[tidPos:i], 10, 32)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to parse pid 3: %v\n", ln)
					continue
				}
				tids[tid] = pid

				pos := strings.Index(ln, " prev_pid=")
				if pos == -1 {
					fmt.Fprintf(os.Stderr, "failed to parse pid 4: %v\n", ln)
					continue
				}
				pos += len(" prev_pid=")
				i = pos
				for ; ln[i] != ' '; i++ {
				}
				ptid, err := strconv.ParseUint(ln[pos:i], 10, 32)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to parse pid 5: %v\n", ln)
					continue
				}
				ppid := tids[ptid]
				if ppid == 0 {
					ppid = ptid
				}

				pos = strings.Index(ln, " next_pid=")
				if pos == -1 {
					fmt.Fprintf(os.Stderr, "failed to parse pid 6: v\n", ln)
					continue
				}
				pos += len(" next_pid=")
				i = pos
				for ; ln[i] != ' '; i++ {
				}
				ntid, err := strconv.ParseUint(ln[pos:i], 10, 32)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to parse pid 7: v\n", ln)
					continue
				}
				npid := tids[ntid]
				if npid == 0 {
					npid = ntid
				}

				p := getProc(int(ppid))
				if p.run > 0 {
					p.run--
				}

				p = getProc(int(npid))
				p.run++
			} else if strings.Contains(ln, " cycles:") {
				i := 0
				for ; ln[i] < '0' || ln[i] > '9'; i++ {
				}
				pidPos := i
				for ; ln[i] >= '0' && ln[i] <= '9'; i++ {
				}
				pid, err := strconv.ParseUint(ln[pidPos:i], 10, 32)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to parse pid 8: %v '%v'\n", ln, ln[pidPos:i])
					continue
				}
				if ln[i] != '/' {
					fmt.Fprintf(os.Stderr, "failed to parse pid 9: %v\n", ln)
					continue
				}
				i++
				tidPos := i
				for ; ln[i] >= '0' && ln[i] <= '9'; i++ {
				}
				tid, err := strconv.ParseUint(ln[tidPos:i], 10, 32)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to parse pid 10: %v\n", ln)
					continue
				}
				tids[tid] = pid
				if *flagPid != 0 && uint64(*flagPid) != pid {
					continue
				}
				p := getProc(int(pid))
				if false && (p.run < 0 || p.run > 2) {
					continue
				}
				p.load[p.run]++
				frames := parseStack(s)
				frames = append(frames, &Frame{uint64(p.run), fmt.Sprintf("LOAD %v", p.run)})
				stkHash := hashStack(frames)
				stack := stacks[stkHash]
				if stack == nil {
					stack = &Stack{
						frames: make([]*profile.Location, len(frames)),
					}
					for i, f := range frames {
						loc := locs[f.pc]
						if loc == nil {
							fn := funcs[f.fn]
							if fn == nil {
								fname := string(append([]byte{}, f.fn...))
								fn = &profile.Function{
									ID:         uint64(len(funcs) + 1),
									Name:       fname,
									SystemName: fname,
								}
								funcs[fname] = fn
							}
							loc = &profile.Location{
								ID:      uint64(len(locs) + 1),
								Address: f.pc,
								Line: []profile.Line{
									profile.Line{
										Function: fn,
										Line:     1,
									},
								},
							}
							locs[f.pc] = loc
						}
						stack.frames[i] = loc
					}
					stacks[stkHash] = stack
				}
				sample := p.samples[stkHash]
				if sample == nil {
					sample = &Sample{
						run:   p.run,
						stack: stack,
					}
					p.samples[stkHash] = sample
				}
				if sample.run != p.run {
					fmt.Fprintf(os.Stderr, "misaccounted sample: %v -> %v\n", p.run, sample.run)
				}
				sample.n++
				p.n++
			}
		}
		done <- s.Err()
	}()
	if err := perf.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warnings: perf failed: %v\n", err)
	}
	if err := <-done; err != nil {
		failf("failed to parse perf output: %v", err)
	}
	var proc *Proc
	max := 0
	for _, p := range procs {
		if max < p.n {
			max = p.n
			proc = p
		}
	}
	maxRun := 0
	for run := range proc.load {
		if maxRun < run {
			maxRun = run
		}
	}
	for _, s := range proc.samples {
		if s.run == 0 {
			s.run = 1
		}
	}
	if *flagRealtime {
		proc.n = 0
		proc.load = make(map[int]int)
		for _, s := range proc.samples {
			s.n = int(float64(s.n) * float64(maxRun) / float64(s.run))
			if s.n < 0 {
				println("underflow:", s.n, maxRun, s.run, int(float64(s.n)*float64(maxRun)/float64(s.run)))
			}
			if proc.n > proc.n+s.n {
				println("overflow:", proc.n, s.n, s.run)
			}
			proc.n += s.n
			proc.load[s.run] += s.n
		}
	}
	maxN := 0
	total := 0
	totalLoad := 0
	load := make([]int, maxRun+1)
	for run, n := range proc.load {
		load[run] = n
		total += n
		totalLoad += run * n
		if maxN < n {
			maxN = n
		}
	}
	fmt.Printf("pid=%v samples=%v avgload=%.1f\n", proc.pid, proc.n, float64(totalLoad)/float64(total))
	for run, n := range load {
		if run == 0 {
			continue
		}
		fmt.Printf("%2v [%5.2f%%]: %v\n", run, float64(n)/float64(total)*100, strings.Repeat("*", int(float64(n)/float64(maxN)*100+0.5)))
	}

	p := &profile.Profile{
		Period:     250000,
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		SampleType: []*profile.ValueType{
			{Type: "samples", Unit: "count"},
			{Type: "cpu", Unit: "nanoseconds"},
		},
	}
	locs := make(map[uint64]bool)
	funcs := make(map[uint64]bool)
	for _, s := range proc.samples {
		p.Sample = append(p.Sample, &profile.Sample{
			Value:    []int64{int64(s.n), int64(s.n) * p.Period},
			Location: s.stack.frames,
		})
		for _, loc := range s.stack.frames {
			if !locs[loc.ID] {
				locs[loc.ID] = true
				p.Location = append(p.Location, loc)
			}
			for _, line := range loc.Line {
				if !funcs[line.Function.ID] {
					funcs[line.Function.ID] = true
					p.Function = append(p.Function, line.Function)
				}
			}
		}
	}

	buff := bufio.NewWriter(f)
	p.Write(buff)
	buff.Flush()
	f.Close()

	exec.Command("go", "tool", "pprof", "-web", "-nodefraction=0.001", "-edgefraction=0.001", f.Name()).Run()
}

func parseStack(s *bufio.Scanner) []*Frame {
	var frames []*Frame
	for s.Scan() && s.Text() != "" {
		ln := s.Text()
		i := 0
		for ; ln[i] == ' ' || ln[i] == '\t'; i++ {
		}
		pos := i
		for ; ln[i] != ' ' && ln[i] != '\t'; i++ {
		}
		pc, err := strconv.ParseUint(ln[pos:i], 16, 64)
		if err != nil {
			break
		}
		fn := ln[i+1:]
		if fn == "[unknown]" {
			continue
		}
		frames = append(frames, &Frame{pc, fn})
	}
	return frames
}

func hashStack(frames []*Frame) uint64 {
	buf := new(bytes.Buffer)
	for _, f := range frames {
		binary.Write(buf, binary.LittleEndian, f.pc)
	}
	s := sha1.Sum(buf.Bytes())
	r := bytes.NewReader(s[:])
	var id uint64
	binary.Read(r, binary.LittleEndian, &id)
	return id
}

func failf(what string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, what+"\n", args...)
	os.Exit(1)
}
