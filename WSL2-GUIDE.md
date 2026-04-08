# Using Hull with WSL2 (Windows) 🪟

Hull is designed natively for Linux, which means the core engine works flawlessly inside WSL2 (Windows Subsystem for Linux). Docker, Caddy, and all your scaffolding commands will execute perfectly.

However, because you are browsing from **Windows** while the app runs in **Linux**, Windows is completely blind to the custom DNS routing and SSL certificates Hull created. 

To get that smooth `https://myapp.test` experience in your Windows browser, you just need to bridge the gap manually.

---

## 1. Network Routing (The Hosts File)

Hull configures `dnsmasq` inside Linux to automatically route `*.test` domains to your local server. Windows does not support wildcard local DNS out of the box, so you must explicitly tell Windows where to find your app.

Every time you run `hull new myapp`, you need to add it to your Windows Hosts file.

1. Open **Notepad** as an **Administrator**.
2. Go to **File -> Open** and navigate to: `C:\Windows\System32\drivers\etc\hosts` *(Note: you may need to change the file filter from "Text Documents" to "All Files" to see it).*
3. Add a new line at the bottom for your app, and one for the global database manager:
   ```text
   127.0.0.1    myapp.test db.test
   ```
4. Save the file.

---

## 2. Browser Trust (The SSL Certificate)

Hull automatically creates a secure Root CA inside Linux so you don't get SSL warnings. However, Chrome and Edge on Windows use the **Windows Certificate Store**. You need to copy the certificate from Linux into Windows.

You only have to do this **once**.

1. Open Windows File Explorer.
2. In the address bar, navigate to your WSL instance. It will look something like this (replace `Ubuntu` and `username` with your actual setup):
   `\\wsl$\Ubuntu\home\username\.hull\system\caddy\caddy-root.crt`
3. Copy that `caddy-root.crt` file to your Windows Desktop.
4. **Double-click** the certificate file on your Desktop and click **Install Certificate**.
5. Select **Local Machine** and click Next.
6. Select **Place all certificates in the following store** and click **Browse**.
7. Choose **Trusted Root Certification Authorities** and click OK.
8. Click Next, then Finish.

Restart your browser, and `https://myapp.test` will now load securely with a green padlock!
