package prometheus

import "fmt"

func kafkaDrain(w window) string {
	return fmt.Sprintf("sum by (%[1]s) (kafka_consumergroup_lag) / (sum by (%[1]s) (deriv(kafka_consumergroup_current_offset[%[2]s])) > 0) and sum by (%[1]s) (resets(kafka_consumergroup_current_offset[%[2]s])) == 0 and min by (%[1]s) (min_over_time(kafka_consumergroup_current_offset[%[2]s])) >= 0", kafkaBy, w.promql)
}

func kafkaStalled(w window) string {
	return fmt.Sprintf("(sum by (%[1]s) (kafka_consumergroup_lag) > 0 and max by (%[1]s) (changes(kafka_consumergroup_current_offset[%[2]s])) == 0 and min by (%[1]s) (min_over_time(kafka_consumergroup_current_offset[%[2]s])) >= 0) * 0 + %[3]g", kafkaBy, w.promql, w.seconds)
}

// Aggregate each name separately: timestamp() removes __name__, so a regex
// spanning both variants could create duplicate labelsets before aggregation.
// Prefer detailed metrics when both endpoints are scraped; never sum them twice.
func rabbitGauge(suffix string) string {
	return fmt.Sprintf("max by (vhost, queue) (rabbitmq_detailed_queue_%[1]s) or max by (vhost, queue) (rabbitmq_queue_%[1]s)", suffix)
}

func rabbitTime(suffix string) string {
	return fmt.Sprintf("min by (vhost, queue) (timestamp(rabbitmq_detailed_queue_%[1]s)) or min by (vhost, queue) (timestamp(rabbitmq_queue_%[1]s))", suffix)
}

func rabbitSubject(m map[string]string) ([]string, []string) {
	if m["queue"] == "" {
		return nil, nil
	}
	if m["vhost"] != "" && m["vhost"] != "/" {
		// A nondefault vhost needs an explicit scoped alias. Do not silently
		// attach another tenant's queue to a bare queue name.
		return []string{m["vhost"] + "/" + m["queue"]}, nil
	}
	return []string{m["queue"]}, nil
}
