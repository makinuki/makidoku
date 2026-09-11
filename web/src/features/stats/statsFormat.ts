import type { ReadingDay } from "../../types";

export function formatReadingTime(seconds: number): string {
  if (seconds <= 0) return "0 min";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  if (hours < 24) return remainder ? `${hours}h ${remainder}m` : `${hours}h`;
  const days = Math.floor(hours / 24);
  const restHours = hours % 24;
  return restHours ? `${days}d ${restHours}h` : `${days}d`;
}

export type DailyBar = { date: string; seconds: number; height: number };

// Scales each day against the busiest day so the chart can render as
// percentages without knowing its pixel height.
export function dailyBars(days: ReadingDay[]): DailyBar[] {
  const max = days.reduce((value, day) => Math.max(value, day.seconds), 0);
  return days.map((day) => ({
    ...day,
    height: max > 0 ? Math.round((day.seconds / max) * 100) : 0,
  }));
}
