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

	// Metric, if defined is used for numbers
	// to specify a prometheus type of counter or gauge
	Metric string `yaml:"metric"`

	// gauge for storing metrics
	gauge prometheus.Gauge
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
}

// Config is used to configure the runner
type Config struct {
	CfgYamlFile string

	xSets *[]XSet
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

	runner.cfg.xSets = &[]XSet{}

	if cfg.CfgYamlFile != "" {
		ymlData, err := ioutil.ReadFile(cfg.CfgYamlFile)
		if err != nil {
			return runner, err
		}

		err = yaml.Unmarshal([]byte(ymlData), &runner.cfg.xSets)
		if err != nil {
			return runner, err
		}
	}

	return runner, nil
}

// Run the sets defined in the configuration.
// a message channel for log output of this
// potentially infinite process.
func (r *Runner) Run() (chan string, error) {

	xerMessages := make(chan string, 0)

	// Loops though sets and Run them.
	go func() {
		var wg sync.WaitGroup

		for _, set := range *r.cfg.xSets {
			xerMessages <- "Run: " + set.Name
			wg.Add(1)
			go runSet(set, xerMessages, &wg)
		}
		xerMessages <- "Done..."

		wg.Wait()
		close(xerMessages)
	}()

	return xerMessages, nil
}

type ResultPackage struct {
	Name    string                 `json:"name"`
	Results map[string]interface{} `json:"results"`
}

func runSet(set XSet, msg chan string, wg *sync.WaitGroup) {

	// http client for set
	netClient := &http.Client{
		Timeout: time.Second * 2,
	}

	polls := promauto.NewCounter(prometheus.CounterOpts{
		Name: fmt.Sprintf("xer_%s_total_polls", set.Name),
		Help: fmt.Sprintf("Total number polls for set %s", set.Name),
	})

	pollErrors := promauto.NewCounter(prometheus.CounterOpts{
		Name: fmt.Sprintf("xer_%s_total_errors", set.Name),
		Help: fmt.Sprintf("Total number polling errors for set %s", set.Name),
	})

	// build metric collectors
	// loop through endpoints to find metrics
	for i, ep := range set.Endpoints {
		if ep.Type == "number" && ep.Metric == "gauge" {

			desc := fmt.Sprintf("%s metric %s", set.Name, ep.Name)

			if ep.Description != "" {
				desc = fmt.Sprintf("%s: %s", set.Name, ep.Description)
			}

			set.Endpoints[i].gauge = promauto.NewGauge(prometheus.GaugeOpts{
				Name: fmt.Sprintf("xer_%s_%s", set.Name, ep.Name),
				Help: desc,
			})

			continue
		}

	}

	epRes := make(map[string]interface{})

	// data structure to store results
	resPkg := ResultPackage{
		Name:    set.Name,
		Results: epRes,
	}

	for {
		msg <- fmt.Sprintf("Running %s", set.Name)

		// gather data by looping through all the endpoints
		// collect the data into a map
		for _, ep := range set.Endpoints {
			resp, err := netClient.Get(ep.URL)
			if err != nil {
				msg <- "ERROR: " + err.Error()
				pollErrors.Inc()
				continue
			}

			body, err := ioutil.ReadAll(resp.Body)

			resPkg.Results[ep.Name] = string(body)

			// store result
			if ep.Type == "number" {
				resPkg.Results[ep.Name], _ = strconv.ParseFloat(string(body), 64)
			}

			// if the type is a number and metric is gauge
			if ep.Type == "number" && ep.Metric == "gauge" {
				gaugeMetric, err := strconv.ParseFloat(string(body), 64)
				if err != nil {
					msg <- "ERROR: " + err.Error()
					pollErrors.Inc()
					break
				}

				ep.gauge.Set(gaugeMetric)
			}

			err = resp.Body.Close()
			if err != nil {
				msg <- "ERROR: " + err.Error()
			}

		}

		// increment the poll stats for this set
		polls.Inc()

		// Marshal struct into a json byte slice
		js, _ := json.Marshal(&resPkg)

		// dest
		err := sendData(set.Dest.Method, set.Dest.URL, js)
		if err != nil {
			msg <- "ERROR: " + err.Error()
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
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(js))
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
