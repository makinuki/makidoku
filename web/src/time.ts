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
