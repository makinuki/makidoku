import { useEffect, useState } from "react";
import { api } from "../api";

export type DateFormat = "relative" | "absolute";

export function useDateFormat(): DateFormat {
  const [format, setFormat] = useState<DateFormat>("relative");
  useEffect(() => {
    let active = true;
    void api
      .settings()
      .then((items) => {
        if (!active) return;
        const value = items.find((item) => item.key === "appearance.date_format")?.value;
        if (value === "relative" || value === "absolute") setFormat(value);
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, []);
  return format;
}
