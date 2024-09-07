/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package util

import (
	"strings"

	"github.com/rqlite/sql"
)

// ExpressionSQLFilter ensures the where filter doesn't contain bad things (SQL injections) and returns a
// good one could be used as Where() in gorm. It expects just an expression, so no other SQL keys will work
// here. For example:
// * `id=1 AND a in (1,2) ORDER BY i; DROP u;` will become just `"id" = 1 AND "a" IN (1, 2)`
// * `DROP users` - will fail
// * `id = 1 OR lol in (SELECT * FROM users)` - will fail
func ExpressionSQLFilter(filter string) (string, error) {
	reader := strings.NewReader(filter)
	exp, err := sql.NewParser(reader).ParseExpr()
	if err != nil {
		return "", err
	}
	return exp.String(), nil
}
