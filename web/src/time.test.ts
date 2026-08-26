import { describe, expect, it } from "vite-plus/test";
import { formatTimestamp } from "./time";

describe("timestamp formatting", () => {
  it("renders relative timestamps by default", () => {
    expect(formatTimestamp(Math.floor(Date.now() / 1000) - 90, "relative")).toBe("1m ago");
  });

  it("renders an absolute timestamp when requested", () => {
    expect(formatTimestamp(Date.parse("2024-01-02T03:04:00Z") / 1000, "absolute")).toContain(
      "2024",
    );
  });
});
