// relativeTime renders a unix timestamp as a coarse human-readable offset.
export function relativeTime(unix: number) {
  const seconds = Math.max(0, Math.floor(Date.now() / 1000) - unix);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 365) return `${days}d ago`;
  return "over a year ago";
}

export function formatTimestamp(unix: number, format: "relative" | "absolute" = "relative") {
  if (format === "absolute") return new Date(unix * 1000).toLocaleString();
  return relativeTime(unix);
}

export function dateGroupLabel(unix: number) {
  const date = new Date(unix * 1000);
  const today = new Date();
  const days = Math.floor(
    (today.setHours(0, 0, 0, 0) - new Date(date).setHours(0, 0, 0, 0)) / 86400000,
  );
  if (days <= 0) return "Today";
  if (days === 1) return "Yesterday";
  return `${days} days ago`;
}
