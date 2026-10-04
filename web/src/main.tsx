import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { SWRConfig } from "swr";
import App from "./App";
import "./app.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <SWRConfig value={{ revalidateOnFocus: false, dedupingInterval: 30_000, errorRetryCount: 3 }}>
      <App />
    </SWRConfig>
  </StrictMode>,
);
