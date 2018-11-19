package xer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"gopkg.in/yaml.v2"
)

// Endpoint specifies a data collection point for
// a set of data.
type Endpoint struct {
	// Name defines the data or metric gathered
	// from this endpoint.
	Name string `yaml:"name"`

	// Description is human readable text about the data,
	// used in HELP section of prometheus metrics.
	Description string `yaml:"description"`

	// URL to GET data
	URL string `yaml:"url"`

	// Type defines the data type as on of
	// text or number
	Type string `yaml:"type"`
}

// Dest defines a location and method for sending
// a collected data set to a destination.
type Dest struct {
	URL string `yaml:"url"`

	// Method defines an HTTP method such as
	// POST or PUT
	Method string `yaml:"method"`
}

// XSet defines a name, destination and polling frequency
// for a set of data collection endpoints.
type XSet struct {
	// Name is used for the key in the collection map
	// example "service-a", "device-b".
	Name string `yaml:"name"`

	// Frequency is the number of seconds to pause
	// between endpoint polling
	Frequency int `yaml:"frequency"`

	Endpoints []Endpoint `yaml:"endpoints"`
	Dest      Dest       `yaml:"dest"`

	// counter for storing set metrics
	epPollCounter *prometheus.CounterVec

	// counter for storing scraping error metrics
	epPollErrorCounter *prometheus.CounterVec

	// counter for storing send error metrics
	sendErrorCounter *prometheus.CounterVec

	// how many times has the set been run
	totalSetRuns *prometheus.CounterVec

	// timing scrapes
	scrapeTiming *prometheus.SummaryVec

	// timing sends
	sendTiming *prometheus.SummaryVec

	// gauges for endpoints by endpoint name
	epGauges map[string]*prometheus.GaugeVec
}

type SetCfg struct {
	Sets []XSet `yaml:"sets"`
}

// Config is used to configure the runner
type Config struct {
	CfgYamlFile string

	xSets []XSet
}

// Runner starts stop and controls XSets.
type Runner struct {
	cfg Config
}

// NewRunner created a Runner from a Config
func NewRunner(cfg Config) (*Runner, error) {
	runner := &Runner{
		cfg: cfg,
	}

	if cfg.CfgYamlFile != "" {
		ymlData, err := ioutil.ReadFile(cfg.CfgYamlFile)
		if err != nil {
			return runner, err
		}

		setCfg := &SetCfg{}

		err = yaml.Unmarshal([]byte(ymlData), &setCfg)
		if err != nil {
			return runner, err
		}

		runner.cfg.xSets = setCfg.Sets
	}

	return runner, nil
}

// Run the sets defined in the configuration.
// a message channel for log output of this
// potentially infinite process.
func (r *Runner) Run() (chan string, chan error, error) {

	xerMessages := make(chan string, 0)
	xerErrorMessages := make(chan error, 0)

	labels := []string{"set"}

	setEpPollCounter := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xer_total_ep_polls",
			Help: "Total endpoint polls for a set.",
		},
		labels,
	)

	setEpPollErrorCounter := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xer_total_set_ep_poll_errors",
			Help: "Total endpoint polling errors for a set.",
		},
		labels,
	)

	sendErrorCounter := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xer_total_send_poll_errors",
			Help: "Total errors attempting to send.",
		},
		labels,
	)

	setRunsCounter := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xer_total_set_runs",
			Help: "Total set runs.",
		},
		labels,
	)

	scrapeTiming := promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "xer_set_scrapetime",
			Help: "The time it took to scrape endpoints for a set.",
		},
		labels,
	)

	sendTiming := promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "xer_set_sendtime",
			Help: "The time it took to send data to dest.",
		},
		labels,
	)

	// Loop through sets and endpoints, find number endpoints
	endpointNameMap := map[string]bool{}

	for _, set := range r.cfg.xSets {
		for _, ep := range set.Endpoints {
			if ep.Type == "number" {
				endpointNameMap[ep.Name] = true
			}
		}
	}

	epGauges := make(map[string]*prometheus.GaugeVec, 0)
	for epName := range endpointNameMap {
		epGauges[epName] = promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("xer_ep_%s", epName),
				Help: fmt.Sprintf("Value for endpoint %s", epName),
			},
			[]string{"set"},
		)
	}

	// Loops though sets and assign counters and gauges
	for i := range r.cfg.xSets {
		r.cfg.xSets[i].epPollCounter = setEpPollCounter
		r.cfg.xSets[i].epPollErrorCounter = setEpPollErrorCounter
		r.cfg.xSets[i].sendErrorCounter = sendErrorCounter
		r.cfg.xSets[i].totalSetRuns = setRunsCounter
		r.cfg.xSets[i].epGauges = epGauges
		r.cfg.xSets[i].scrapeTiming = scrapeTiming
		r.cfg.xSets[i].sendTiming = sendTiming
	}

	// Loops though sets and Run them.
	go func() {
		var wg sync.WaitGroup

		for _, set := range r.cfg.xSets {
			xerMessages <- "Run: " + set.Name
			wg.Add(1)

			go runSet(set, xerMessages, xerErrorMessages, &wg)
		}
		xerMessages <- "Done..."

		wg.Wait()
		close(xerMessages)
		close(xerErrorMessages)
	}()

	return xerMessages, xerErrorMessages, nil
}

type ResultPackage struct {
	Name    string                 `json:"name"`
	Results map[string]interface{} `json:"results"`
}

func runSet(set XSet, msg chan string, errmsg chan error, wg *sync.WaitGroup) {

	// http client for set
	netClient := &http.Client{
		Timeout: time.Second * 2,
	}

	epRes := make(map[string]interface{})

	// data structure to store results
	resPkg := ResultPackage{
		Name:    set.Name,
		Results: epRes,
	}

	// gauge for storing values by set labeled with endpoint name
	//
	//    xer_set_beta{ep="curve_a"} 48
	//    xer_set_beta{ep="curve_b"} 0.4333
	//    xer_set_beta{ep="sec"} 12
	//    xer_set_beta{ep="time"} 1.542483792e+09
	//
	gaugeSetByEp := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("xer_set_%s", set.Name),
			Help: fmt.Sprintf("Endpoint values for %s.", set.Name),
		},
		[]string{"ep"},
	)

	for {
		start := time.Now()

		msg <- fmt.Sprintf("Running %s", set.Name)

		// increment the poll stats for this set
		set.totalSetRuns.With(prometheus.Labels{"set": set.Name}).Inc()

		// gather data by looping through all the endpoints
		// collect the data into a map
		for _, ep := range set.Endpoints {
			resp, err := netClient.Get(ep.URL)
			if err != nil {
				errmsg <- err
				set.epPollErrorCounter.With(prometheus.Labels{"set": set.Name}).Inc()
				continue
			}

			body, err := ioutil.ReadAll(resp.Body)

			resPkg.Results[ep.Name] = string(body)

			// store result
			if ep.Type == "number" {
				resPkg.Results[ep.Name], _ = strconv.ParseFloat(string(body), 64)
			}

			// if the type is a number use it as a prometheus
			// metric
			if ep.Type == "number" {
				gaugeMetric, err := strconv.ParseFloat(string(body), 64)
				if err != nil {
					errmsg <- err
					set.epPollErrorCounter.With(prometheus.Labels{"set": set.Name}).Inc()
					break
				}

				gaugeSetByEp.With(prometheus.Labels{"ep": ep.Name}).Set(gaugeMetric)

				if gauge, ok := set.epGauges[ep.Name]; ok {
					gauge.With(prometheus.Labels{"set": set.Name}).Set(gaugeMetric)
				}

			}

			set.epPollCounter.With(prometheus.Labels{"set": set.Name}).Inc()

			err = resp.Body.Close()
			if err != nil {
				errmsg <- err
				set.epPollErrorCounter.With(prometheus.Labels{"set": set.Name}).Inc()
			}

		}

		elapsed := time.Since(start)

		set.scrapeTiming.With(prometheus.Labels{"set": set.Name}).Observe(elapsed.Seconds())

		// Marshal struct into a json byte slice
		js, _ := json.Marshal(&resPkg)

		// dest
		if set.Dest.Method != "" && set.Dest.URL != "" {
			start = time.Now()
			err := sendData(set.Dest.Method, set.Dest.URL, js)
			if err != nil {
				errmsg <- err
				set.sendErrorCounter.With(prometheus.Labels{"set": set.Name}).Inc()
			}

			elapsed := time.Since(start)
			set.sendTiming.With(prometheus.Labels{"set": set.Name}).Observe(elapsed.Seconds())
		}

		// wait based on a defined frequency
		msg <- fmt.Sprintf("Ran %s and now waiting %d seconds.", set.Name, set.Frequency)
		<-time.After(time.Duration(set.Frequency) * time.Second)
	}

	wg.Done()
}

func sendData(method string, url string, js []byte) error {
	// http client for set
	netClient := &http.Client{
		Timeout: time.Second * 1,
	}

	// Post JSON
	req, err := http.NewRequest(method, url, bytes.NewBuffer(js))
	if err != nil {
		return err
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return err
	}

	err = resp.Body.Close()
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("non-200 response from server, got %d", resp.StatusCode))
	}

	return nil
}
