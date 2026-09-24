import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { initInstallPrompt } from "./app/installPrompt";
import "./index.css";

// Capture starts before first render so a beforeinstallprompt fired during
// script evaluation is not missed.
initInstallPrompt();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </React.StrictMode>,
);
