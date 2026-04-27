#!/bin/bash
set -euo pipefail

TARGET_DIR=$1
SITE_NAME=$2

echo "Bootstrapping plain PHP environment for $SITE_NAME..."

cat << 'EOF' > "$TARGET_DIR/index.php"
<!DOCTYPE html>
<html>
<head>
    <title>Hull is Live</title>
    <style>
        body { font-family: system-ui, sans-serif; text-align: center; margin-top: 50px; background: #f9fafb; color: #111827; }
        h1 { color: #2563eb; }
        hr { border: 0; border-top: 1px solid #e5e7eb; margin: 40px auto; max-width: 600px; }
        .phpinfo { max-width: 800px; margin: 0 auto; text-align: left; background: white; padding: 20px; border-radius: 8px; box-shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1); }
        .phpinfo table { width: 100%; border-collapse: collapse; }
        .phpinfo th, .phpinfo td { border: 1px solid #e5e7eb; padding: 8px; }
        .phpinfo th { background-color: #f3f4f6; }
    </style>
</head>
<body>
    <h1>🚀 Hull is Live</h1>
    <p>Your plain PHP environment <strong><?php echo $_SERVER['HTTP_HOST']; ?></strong> is running successfully.</p>
    <hr>
    <div class="phpinfo">
        <?php
            ob_start();
            phpinfo(INFO_GENERAL | INFO_ENVIRONMENT | INFO_MODULES);
            $info = ob_get_contents();
            ob_end_clean();

            // Strip the inline CSS/HTML wrapper from raw phpinfo for a cleaner embedded look
            $info = preg_replace('%^.*<body>(.*)</body>.*$%ms','$1', $info);
            echo $info;
        ?>
    </div>
</body>
</html>
EOF

echo "Plain PHP index.php generated successfully."