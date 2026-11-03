package credential

import (
	"fmt"
	"strconv"
	"time"
)

func positiveInt(v string) error {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return err
	}
	if n < 1 {
		return fmt.Errorf("must be at least 1")
	}
	return nil
}

func positiveDuration(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil {
		return err
	}
	if d <= 0 {
		return fmt.Errorf("must be greater than 0")
	}
	return nil
}
