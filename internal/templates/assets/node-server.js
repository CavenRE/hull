// Minimal Hull Node app. Serves a page on $PORT with the standard library.
// Add dependencies to package.json, then `hull restart` (or `hull exec npm
// install <pkg>`). Edit this file and `hull restart` to pick up changes.
const http = require("http");

const PORT = process.env.PORT || 8000;

const server = http.createServer((req, res) => {
  const db = process.env.DATABASE_URL ? "<p>DATABASE_URL is set, ready to connect.</p>" : "";
  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end(
    `<h1>Your Node app is live</h1><p>Node ${process.version}</p>${db}` +
      "<p>Edit <code>server.js</code>, then <code>hull restart</code>.</p>"
  );
});

server.listen(PORT, "0.0.0.0", () => console.log(`listening on :${PORT}`));
