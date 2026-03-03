/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package retryutil

import (
	"context"
	"fmt"
	"time"
)

type RetryOkError error

// Retry retries f every interval until the context si closed or times out.
// The interval won't be affected by how long f takes.
// For example, if interval is 3s, f takes 1s, another f will be called 2s later.
// However, if f takes longer than interval, it will be delayed.
func Retry(ctx context.Context, interval time.Duration, f func() (bool, error)) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		ok, err := f()
		if err != nil {
			// Ignore error's when expected during retryOnErr
			_, shouldRetry := err.(RetryOkError)
			if !shouldRetry {
				return err
			}
		}
		if ok {
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fmt.Errorf("%v: %v", ctx.Err(), err)
		}
	}
}

// Retry function that can return an error.
func RetryOnErr(ctx context.Context, interval time.Duration, f func() error) error {
	return Retry(ctx, interval, func() (bool, error) {

		// run f() and check for err
		if err := f(); err != nil {
			return false, RetryOkError(err)
		}

		// ok
		return true, nil
	})
}
