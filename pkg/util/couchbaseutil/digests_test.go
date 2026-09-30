/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestImageDigestsConcurrent learns and reads digests from many goroutines at once, as
// concurrent reconciles do after an operator restart. Run with -race.
func TestImageDigestsConcurrent(t *testing.T) {
	var wg sync.WaitGroup

	for i := 0; i < 32; i++ {
		digest := strings.Repeat(fmt.Sprintf("%x", i%16), 64)
		image := "couchbase/server@sha256:" + digest

		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				if v, _ := UpdateImageDigestMap(image, "7.6.2"); v != "couchbase-7.6.2" {
					t.Errorf("learned %q, want couchbase-7.6.2", v)
				}

				if v := GetSHA256Version(digest); v != "couchbase-7.6.2" {
					t.Errorf("read %q, want couchbase-7.6.2", v)
				}
			}
		}()
	}

	wg.Wait()
}
