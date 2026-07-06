package worker

import "errors"

var ErrJobCancelled = errors.New("job cancelled")

func IsJobCancelled(err error) bool {
	return errors.Is(err, ErrJobCancelled)
}
