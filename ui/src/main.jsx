import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Kumo's stylesheet must be imported before index.css so that local styles
// win over Kumo's utility classes at equal specificity.
import "@cloudflare/kumo/styles/standalone";
import "./index.css";
import App from "./App.jsx";

createRoot(document.getElementById("root")).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
