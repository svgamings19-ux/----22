"use strict";
document.querySelectorAll("nav a").forEach(link => {
  if (link.pathname === window.location.pathname) link.setAttribute("aria-current", "page");
});
