package main

import (
	"encoding/csv"
	"flag"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/perf/benchstat"
)

var (
	commitDates = flag.String("commit-dates", "", "file containing `git log --format=\"format:%H,%cD\" --date-order`")
	metricName  = flag.String("metric", "ns/op", "metric to fetch")
	regex       = flag.String("regex", ".", "benchmarks to include")
	relative    = flag.Bool("relative", false, "whether to use relative results")
	rolling     = flag.Int("rolling", 0, "number of previous commits to compare to")
)

const rfc2822 = "Mon, 2 Jan 2006 15:04:05 -0700"

// Get started with:
// * mkdir -p /tmp/bench
// * gsutil -m cp -r 'gs://istio-prow/benchmarks/*.txt' /tmp/bench
// * git log --format="format:%H,%cD" --date-order > /tmp/bench/commits
// * go run main.go --commit-dates=/tmp/bench/commits /tmp/bench/*.txt
func main() {
	flag.Parse()

	goodDataStart, err := time.Parse("2006-01-02", "2020-07-18")
	if err != nil {
		log.Fatal(err)
	}

	reg := regexp.MustCompile(*regex)

	if commitDates == nil || *commitDates == "" {
		log.Fatal("require commit-dates")
	}
	c := &benchstat.Collection{
		DeltaTest: benchstat.UTest,
		Alpha:     0.05,
	}
	f, err := ioutil.ReadFile(*commitDates)
	if err != nil {
		log.Fatal(err)
	}

	commitToDate := map[string]time.Time{}
	dateToCommit := map[time.Time]string{}
	for _, l := range strings.Split(string(f), "\n") {
		if len(l) == 0 {
			continue
		}
		spl := strings.SplitN(l, ",", 2)
		if len(spl) != 2 {
			log.Fatalf("unexpected split from %v\n", l)
		}
		tm, err := time.Parse(rfc2822, spl[1])
		if err != nil {
			log.Fatalf("failed to parse from %v: %v\n", l, err)
		}
		commitToDate[spl[0]] = tm
		dateToCommit[tm] = spl[0]
	}
	files := flag.Args()
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			log.Fatal(err)
		}
		if err := c.AddFile(file, f); err != nil {
			log.Fatal(err)
		}
		f.Close()
	}

	fileToDate := func(file string) time.Time {
		sha := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		return commitToDate[sha]
	}

	results := map[string]map[time.Time]Result{}
	for _, t := range c.Tables() {
		for _, row := range t.Rows {
			for i, metric := range row.Metrics {
				if metric.Unit != *metricName {
					continue
				}
				if !reg.MatchString(row.Benchmark) {
					continue
				}
				date := fileToDate(files[i])
				// Todo make this configurable
				if date.Before(goodDataStart) {
					continue
				}
				result := Result{Name: row.Benchmark, Date: date, Nanoseconds: metric.Mean}
				if _, f := results[row.Benchmark]; !f {
					results[row.Benchmark] = map[time.Time]Result{}
				}
				results[row.Benchmark][date] = result
			}
		}
	}

	testKeys := []string{}
	for k := range results {
		testKeys = append(testKeys, k)
	}
	sort.Strings(testKeys)

	dateSet := map[time.Time]struct{}{}
	for _, k := range testKeys {
		for date := range results[k] {
			dateSet[date] = struct{}{}
		}
	}
	dateKeys := []time.Time{}
	for date := range dateSet {
		dateKeys = append(dateKeys, date)
	}
	sort.Slice(dateKeys, func(i, j int) bool {
		return dateKeys[i].Before(dateKeys[j])
	})

	firstResult := map[string]float64{}
	for _, date := range dateKeys {
		for _, test := range testKeys {
			if _, f := firstResult[test]; f {
				continue
			}
			if almostEqual(results[test][date].Nanoseconds, 0) {
				continue
			}
			firstResult[test] = results[test][date].Nanoseconds
		}
	}
	rollingResults := map[string][]float64{}

	w := csv.NewWriter(os.Stdout)
	w.Write(append([]string{"SHA", "Date"}, testKeys...))
	for _, date := range dateKeys {
		row := []string{dateToCommit[date], date.String()}
		for _, test := range testKeys {
			res := results[test][date].Nanoseconds
			if *relative {
				res = res / firstResult[test]
				if almostEqual(res, 0) {
					res = 1
				}
			} else if *rolling > 0 {
				if len(rollingResults[test]) > 0 {
					res = res / avg(rollingResults[test])
				} else {
					res = 1
				}
				if !almostEqual(results[test][date].Nanoseconds, 0) {
					rollingResults[test] = append(rollingResults[test], results[test][date].Nanoseconds)
					if len(rollingResults[test]) == *rolling {
						rollingResults[test] = rollingResults[test][1:]
					}
				}
			}
			row = append(row, strconv.FormatFloat(res, 'f', -1, 64))
		}
		w.Write(row)
	}
	w.Flush()
}

const float64EqualityThreshold = 1e-8

func avg(a []float64) float64 {
	var sum float64 = 0
	for _, i := range a {
		sum += i
	}
	return sum / float64(len(a))
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= float64EqualityThreshold
}

type Result struct {
	Name        string
	Date        time.Time
	Nanoseconds float64
}
