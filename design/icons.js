/* Hull , inline line-icon set. 1.75px stroke, currentColor.
   icon(name, size=18) -> SVG markup string. */
(function () {
  const P = {
    // nav
    sites:    `<rect x="3" y="4" width="18" height="6" rx="1.5"/><rect x="3" y="14" width="18" height="6" rx="1.5"/><path d="M7 7h.01M7 17h.01"/>`,
    dashboard:`<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>`,
    services: `<path d="M12 2 3 7l9 5 9-5-9-5Z"/><path d="m3 12 9 5 9-5"/><path d="m3 17 9 5 9-5"/>`,
    mail:     `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m4 7 8 6 8-6"/>`,
    logs:     `<path d="M4 17l5-5-5-5"/><path d="M12 19h8"/>`,
    settings: `<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"/>`,
    // actions
    search:   `<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>`,
    plus:     `<path d="M12 5v14M5 12h14"/>`,
    lock:     `<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>`,
    external: `<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5"/>`,
    copy:     `<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>`,
    folder:   `<path d="M4 7a2 2 0 0 1 2-2h3.5l2 2H18a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7Z"/>`,
    editor:   `<path d="m9 8-4 4 4 4"/><path d="m15 8 4 4-4 4"/>`,
    play:     `<path d="M7 4.5v15l13-7.5-13-7.5Z"/>`,
    stop:     `<rect x="6" y="6" width="12" height="12" rx="2"/>`,
    restart:  `<path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 4v5h-5"/>`,
    chevdown: `<path d="m6 9 6 6 6-6"/>`,
    chevright:`<path d="m9 6 6 6-6 6"/>`,
    check:    `<path d="M20 6 9 17l-5-5"/>`,
    alert:    `<path d="M12 9v4M12 17h.01"/><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"/>`,
    x:        `<path d="M18 6 6 18M6 6l12 12"/>`,
    link:     `<path d="M9 17H7A5 5 0 0 1 7 7h2"/><path d="M15 7h2a5 5 0 0 1 0 10h-2"/><path d="M8 12h8"/>`,
    unlink:   `<path d="M9 17H7A5 5 0 0 1 7 7"/><path d="M15 7h2a5 5 0 0 1 3.5 8.5"/><path d="m3 3 18 18"/>`,
    trash:    `<path d="M4 7h16M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2m-9 0 1 13a2 2 0 0 0 2 2h4a2 2 0 0 0 2-2l1-13"/>`,
    sun:      `<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`,
    moon:     `<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z"/>`,
    monitor:  `<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>`,
    // engine categories
    database: `<ellipse cx="12" cy="6" rx="7" ry="3"/><path d="M5 6v12c0 1.7 3.1 3 7 3s7-1.3 7-3V6"/><path d="M5 12c0 1.7 3.1 3 7 3s7-1.3 7-3"/>`,
    cache:    `<path d="M13 2 4 14h7l-1 8 9-12h-7l1-8Z"/>`,
    search2:  `<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>`,
    storage:  `<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>`,
    tool:     `<path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.5 2.5-2.4-2.4 2.5-2.5Z"/>`,
    // health
    server:   `<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>`,
    route:    `<circle cx="6" cy="19" r="3"/><circle cx="18" cy="5" r="3"/><path d="M9 19h6a4 4 0 0 0 0-8H9a4 4 0 0 1 0-8"/>`,
    globe:    `<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18Z"/>`,
    cert:     `<path d="M12 3 4 6v6c0 4 3.4 7.5 8 9 4.6-1.5 8-5 8-9V6l-8-3Z"/><path d="m9 12 2 2 4-4"/>`,
    // misc
    grip:     `<circle cx="9" cy="6" r="1"/><circle cx="15" cy="6" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="18" r="1"/><circle cx="15" cy="18" r="1"/>`,
    clock:    `<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`,
    arrowup:  `<path d="M12 19V5M5 12l7-7 7 7"/>`,
    cube:     `<path d="M12 2 3 7v10l9 5 9-5V7l-9-5Z"/><path d="m3 7 9 5 9-5"/><path d="M12 12v10"/>`,
    winmin:   `<path d="M5 12h14"/>`,
    winmax:   `<rect x="5" y="5" width="14" height="14" rx="1.5"/>`,
    winrestore: `<rect x="8" y="8" width="11" height="11" rx="1.5"/><path d="M8 8V6.5A1.5 1.5 0 0 1 9.5 5H17.5A1.5 1.5 0 0 1 19 6.5V14.5A1.5 1.5 0 0 1 17.5 16H16"/>`,
    sliders:  `<path d="M4 6h10M18 6h2M4 12h2M10 12h10M4 18h12M20 18h0"/><circle cx="16" cy="6" r="2"/><circle cx="8" cy="12" r="2"/><circle cx="18" cy="18" r="2"/>`,
    download: `<path d="M12 3v12m0 0 4-4m-4 4-4-4"/><path d="M5 21h14"/>`,
  };
  window.icon = function (name, size) {
    const s = size || 18;
    const fillIcons = { play: 1, stop: 1 };
    const fill = fillIcons[name] ? 'currentColor' : 'none';
    const sw = fillIcons[name] ? 0 : 1.75;
    return `<svg viewBox="0 0 24 24" width="${s}" height="${s}" fill="${fill}" stroke="currentColor" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${P[name] || ''}</svg>`;
  };
})();
