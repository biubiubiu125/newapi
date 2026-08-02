/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import assert from 'node:assert/strict';
import { describe, test } from 'node:test';

import { toBoolean } from './boolean.js';

describe('toBoolean', () => {
  test('accepts explicit truthy boolean representations', () => {
    assert.equal(toBoolean(true), true);
    assert.equal(toBoolean(1), true);
    assert.equal(toBoolean('true'), true);
    assert.equal(toBoolean('1'), true);
  });

  test('rejects false, empty, and unknown values', () => {
    assert.equal(toBoolean(false), false);
    assert.equal(toBoolean(0), false);
    assert.equal(toBoolean('false'), false);
    assert.equal(toBoolean('yes'), false);
    assert.equal(toBoolean(null), false);
    assert.equal(toBoolean(undefined), false);
  });
});
