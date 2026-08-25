import anilist from "../assets/trackers/anilist.svg";
import kitsu from "../assets/trackers/kitsu.svg";
import mangabaka from "../assets/trackers/mangabaka.webp";
import mangaupdates from "../assets/trackers/mangaupdates.svg";
import myanimelist from "../assets/trackers/myanimelist.svg";

const logos: Record<string, string> = {
  anilist,
  myanimelist,
  kitsu,
  mangabaka,
  mangaupdates,
};

const labels: Record<string, string> = {
  anilist: "AniList",
  myanimelist: "MyAnimeList",
  kitsu: "Kitsu",
  mangabaka: "MangaBaka",
  mangaupdates: "MangaUpdates",
};

export function trackerLabel(name: string) {
  return labels[name.toLowerCase()] || name;
}

export function TrackerLogo({ name, className = "size-10" }: { name: string; className?: string }) {
  const label = trackerLabel(name);
  const src = logos[name.toLowerCase()];
  return src ? (
    <img src={src} alt={`${label} logo`} className={`${className} rounded-lg`} />
  ) : (
    <span
      aria-label={`${label} logo`}
      className={`${className} grid place-items-center rounded-lg bg-zinc-800 text-xs font-semibold text-zinc-300`}
    >
      {label.slice(0, 2).toUpperCase()}
    </span>
  );
}
