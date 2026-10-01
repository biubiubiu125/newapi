import { describe, expect, test } from "vitest";

import { parseTopupAmountInput } from "./amount";

describe("topup amount input", () => {
  test("accepts a whole number and rejects decimals", () => {
    expect(parseTopupAmountInput("12")).toBe(12);
    expect(parseTopupAmountInput("")).toBe(0);
    expect(parseTopupAmountInput("10.5")).toBeNull();
    expect(parseTopupAmountInput("10.")).toBeNull();
  });
});
