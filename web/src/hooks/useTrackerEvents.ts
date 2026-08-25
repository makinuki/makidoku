import { useEffect, useRef } from "react";

export function useTrackerEvents(onEvent: () => void) {
  const callback = useRef(onEvent);
  useEffect(() => {
    callback.current = onEvent;
  }, [onEvent]);
  useEffect(() => {
    let socket: WebSocket | undefined;
    let reconnectTimer: number | undefined;
    let disposed = false;
    const connect = () => {
      if (disposed) return;
      const protocol = location.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(`${protocol}//${location.host}/api/trackers/events`);
      socket.onmessage = (event) => {
        try {
          const message = JSON.parse(String(event.data)) as { type?: string };
          if (message.type === "credentials") callback.current();
        } catch {
          // Ignore malformed frames and wait for the next event.
        }
      };
      socket.onclose = () => {
        if (!disposed) reconnectTimer = window.setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      disposed = true;
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, []);
}
