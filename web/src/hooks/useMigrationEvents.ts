import { useEffect, useRef } from "react";
import type { MigrationFrame, MigrationTitleEvent } from "../types";

type MigrationEventHandlers = {
  onSnapshot?: (titles: MigrationTitleEvent[], done: boolean) => void;
  onTitle?: (title: MigrationTitleEvent) => void;
  onComplete?: () => void;
};

// useMigrationEvents streams one migration job's per-title results. A client
// that reconnects re-attaches to the same job and is sent a snapshot of what
// already resolved, so a dropped socket does not lose progress.
export function useMigrationEvents(jobId: string | null, handlers: MigrationEventHandlers) {
  const callback = useRef(handlers);
  useEffect(() => {
    callback.current = handlers;
  }, [handlers]);
  useEffect(() => {
    if (!jobId) return;
    let socket: WebSocket | undefined;
    let reconnectTimer: number | undefined;
    let disposed = false;
    let finished = false;
    const connect = () => {
      if (disposed || finished) return;
      const protocol = location.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(
        `${protocol}//${location.host}/api/migration/jobs/${encodeURIComponent(jobId)}/events`,
      );
      socket.onmessage = (event) => {
        let frame: MigrationFrame;
        try {
          frame = JSON.parse(String(event.data)) as MigrationFrame;
        } catch {
          // Ignore malformed frames and wait for the next one.
          return;
        }
        if (frame.type === "snapshot") {
          // A finished job is not reconnected; the server closes the socket
          // after this frame.
          if (frame.done) finished = true;
          callback.current.onSnapshot?.(frame.titles ?? [], frame.done === true);
        } else if (frame.type === "title" && frame.title) {
          callback.current.onTitle?.(frame.title);
        } else if (frame.type === "complete") {
          finished = true;
          callback.current.onComplete?.();
        }
      };
      socket.onclose = () => {
        if (!disposed && !finished) reconnectTimer = window.setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      disposed = true;
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [jobId]);
}
