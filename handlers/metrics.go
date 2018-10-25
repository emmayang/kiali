package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/kiali/kiali/kubernetes"
	"github.com/kiali/kiali/log"
	"github.com/kiali/kiali/prometheus"
	"github.com/kiali/kiali/util"
)

func extractMetricsQueryParams(r *http.Request, q *prometheus.MetricsQuery, k8s kubernetes.IstioClientInterface) error {
	q.FillDefaults()
	queryParams := r.URL.Query()
	if rateIntervals, ok := queryParams["rateInterval"]; ok && len(rateIntervals) > 0 {
		// Only first is taken into consideration
		q.RateInterval = rateIntervals[0]
	}
	if rateFuncs, ok := queryParams["rateFunc"]; ok && len(rateFuncs) > 0 {
		// Only first is taken into consideration
		if rateFuncs[0] != "rate" && rateFuncs[0] != "irate" {
			// Bad request
			return errors.New("Bad request, query parameter 'rateFunc' must be either 'rate' or 'irate'")
		}
		q.RateFunc = rateFuncs[0]
	}
	if queryTimes, ok := queryParams["queryTime"]; ok && len(queryTimes) > 0 {
		if num, err := strconv.ParseInt(queryTimes[0], 10, 64); err == nil {
			q.End = time.Unix(num, 0)
		} else {
			// Bad request
			return errors.New("Bad request, cannot parse query parameter 'queryTime'")
		}
	}
	if durations, ok := queryParams["duration"]; ok && len(durations) > 0 {
		if num, err := strconv.ParseInt(durations[0], 10, 64); err == nil {
			duration := time.Duration(num) * time.Second
			q.Start = q.End.Add(-duration)
		} else {
			// Bad request
			return errors.New("Bad request, cannot parse query parameter 'duration'")
		}
	}
	if steps, ok := queryParams["step"]; ok && len(steps) > 0 {
		if num, err := strconv.Atoi(steps[0]); err == nil {
			q.Step = time.Duration(num) * time.Second
		} else {
			// Bad request
			return errors.New("Bad request, cannot parse query parameter 'step'")
		}
	}
	if filters, ok := queryParams["filters[]"]; ok && len(filters) > 0 {
		q.Filters = filters
	}
	if quantiles, ok := queryParams["quantiles[]"]; ok && len(quantiles) > 0 {
		for _, quantile := range quantiles {
			f, err := strconv.ParseFloat(quantile, 64)
			if err != nil {
				// Non parseable quantile
				return errors.New("Bad request, cannot parse query parameter 'quantiles', float expected")
			}
			if f < 0 || f > 1 {
				return errors.New("Bad request, invalid quantile(s): should be between 0 and 1")
			}
		}
		q.Quantiles = quantiles
	}
	if avgFlags, ok := queryParams["avg"]; ok && len(avgFlags) > 0 {
		if avgFlag, err := strconv.ParseBool(avgFlags[0]); err == nil {
			q.Avg = avgFlag
		} else {
			// Bad request
			return errors.New("Bad request, cannot parse query parameter 'avg'")
		}
	}
	if lblsin, ok := queryParams["byLabelsIn[]"]; ok && len(lblsin) > 0 {
		q.ByLabelsIn = lblsin
	}
	if lblsout, ok := queryParams["byLabelsOut[]"]; ok && len(lblsout) > 0 {
		q.ByLabelsOut = lblsout
	}

	// Get namespace info.
	namespace, err := k8s.GetNamespace(q.Namespace)
	if err != nil {
		return err
	}
	// If needed, adjust interval -- Make sure query won't fetch data before the namespace creation
	intervalStartTime, err := util.GetStartTimeForRateInterval(q.End, q.RateInterval)
	if err != nil {
		return err
	}
	if intervalStartTime.Before(namespace.CreationTimestamp.Time) {
		q.RateInterval = fmt.Sprintf("%ds", int(q.End.Sub(namespace.CreationTimestamp.Time).Seconds()))
		intervalStartTime = namespace.CreationTimestamp.Time
		log.Debugf("[extractMetricsQueryParams] Interval set to: %v", q.RateInterval)
	}
	// If needed, adjust query start time (bound to namespace creation time)
	log.Debugf("[extractMetricsQueryParams] Requested query start time: %v", q.Start)
	intervalDuration := q.End.Sub(intervalStartTime)
	allowedStart := namespace.CreationTimestamp.Time.Add(intervalDuration)
	if q.Start.Before(allowedStart) {
		q.Start = allowedStart
		log.Debugf("[extractMetricsQueryParams] Query start time set to: %v", q.Start)

		if q.Start.After(q.End) {
			// This means that the query range does not fall in the range
			// of life of the namespace. So, there are no metrics to query.
			log.Debugf("[extractMetricsQueryParams] Query end time = %v; not querying metrics.", q.End)
			return errors.New("after checks, query start time is after end time")
		}
	}

	// Adjust start & end times to be a multiple of step
	stepInSecs := int64(q.Step.Seconds())
	q.Start = time.Unix((q.Start.Unix()/stepInSecs)*stepInSecs, 0)
	return nil
}
