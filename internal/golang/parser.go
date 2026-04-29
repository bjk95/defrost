package golang

import (
	"io"
	"time"

	"github.com/bjk95/defrost/internal/models"
	"gotest.tools/gotestsum/testjson"
)

type collector struct {
	results  []models.TestResult
	inflight map[string]*models.TestResult
}

func (c *collector) Event(ev testjson.TestEvent, _ *testjson.Execution) error {
	if ev.PackageEvent() {
		return nil
	}
	id := ev.Package + ev.Test
	switch ev.Action {
	case testjson.ActionOutput:
		tr, ok := c.inflight[id]
		if !ok {
			tr = &models.TestResult{Id: id}
			c.inflight[id] = tr
		}
		tr.Output += ev.Output
	case testjson.ActionPass, testjson.ActionFail, testjson.ActionSkip:
		tr, ok := c.inflight[id]
		if !ok {
			tr = &models.TestResult{Id: id}
		}
		d := time.Duration(ev.Elapsed * float64(time.Second))
		tr.Ran = ev.Action != testjson.ActionSkip
		tr.Passed = ev.Action == testjson.ActionPass
		tr.Duration = d
		tr.StartTime = ev.Time.Add(-d)
		c.results = append(c.results, *tr)
		delete(c.inflight, id)
	}
	return nil
}

func (collector) Err(string) error { return nil }

func Parse(r io.Reader) ([]models.TestResult, error) {
	c := &collector{inflight: map[string]*models.TestResult{}}
	if _, err := testjson.ScanTestOutput(testjson.ScanConfig{
		Stdout:  r,
		Handler: c,
	}); err != nil {
		return nil, err
	}
	return c.results, nil
}
