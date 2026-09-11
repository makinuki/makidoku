import { describe, expect, it } from "vite-plus/test";
import { dailyBars, formatReadingTime } from "./statsFormat";

describe("stats formatting", () => {
  it("formats durations across scales", () => {
    expect(formatReadingTime(0)).toBe("0 min");
    expect(formatReadingTime(90)).toBe("1 min");
    expect(formatReadingTime(3600)).toBe("1h");
    expect(formatReadingTime(5400)).toBe("1h 30m");
    expect(formatReadingTime(90000)).toBe("1d 1h");
  });

  it("scales daily bars against the busiest day", () => {
    const bars = dailyBars([
      { date: "2026-09-01", seconds: 60 },
      { date: "2026-09-02", seconds: 120 },
    ]);
    expect(bars.map((bar) => bar.height)).toEqual([50, 100]);
  });

  it("handles a series with no reading time", () => {
    expect(dailyBars([])).toEqual([]);
    expect(dailyBars([{ date: "2026-09-01", seconds: 0 }]).map((bar) => bar.height)).toEqual([0]);
  });
});
