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
	"fmt"
	"testing"
)

var (
	testSQLExpressionInjections = [][2]string{
		{``, ``},
		{`1=1`, `1 = 1`},
		{`id = 3; DROP users`, `"id" = 3`},
		{`a IN (1,2) ORDER BY id; DROP users`, `"a" IN (1, 2)`},
		// Fails
		{`SELECT * FROM users WHERE a = 1; DROP users`, ``}, // Invalid expression
		{`a in (SELECT * FROM users)`, ``},                  // Subquery could be dangerous
	}
)

func Test_expression_sql_filter_where_injections(t *testing.T) {
	for _, sqlAndResult := range testSQLExpressionInjections {
		t.Run(fmt.Sprintf("Testing `%s`", sqlAndResult[0]), func(t *testing.T) {
			out, err := ExpressionSQLFilter(sqlAndResult[0])
			if out != sqlAndResult[1] {
				t.Fatalf("ExpressionSQLFilter(`%s`) = `%s`, %v; want: `%s`", sqlAndResult[0], out, err, sqlAndResult[1])
			}
		})
	}
}
