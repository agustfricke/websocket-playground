# Como hacer un deploy de wss con nginx con https y wss?

-   Suponindo que ya tienes instalado nginx y habilitado los puertos 80, 443 y 8080 en tu
    servidor vamos a pasar directo a la configuracion.
-   Lo primero seria crear un certificado con Let's Encrypt.

```bash
sudo apt install certbot python3-certbot-nginx
certbot --nginx
```

-   Acepta los terminos y condiciones, pon tu email y indica el dominio al que quieres crear un certificado.

-   Ahora en una terminal corre el servidor de Go. El servidor va a escuchar en el puerto 8080.

```bash
go run main.go
```

-   En la ruta **/etc/nginx/sites-available** vas a crear un archivo llamado **websocket-fiber.conf**

```bash
server {
    listen 443 ssl;
    server_name www.tu-dominio.com tu-dominio.com;

    ssl_certificate /etc/letsencrypt/live/tu-dominio.com/fullchain.pem; # managed by Certbot
    ssl_certificate_key /etc/letsencrypt/live/tu-dominio.com/privkey.pem; # managed by Certbot

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }
}
```

-   Fijate de remplazar tu-dominio.com por tu dominio real.

-   Ahora pongamos esta config en sites-enabled y elimines el default de nginx

```bash
sudo rm /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default
sudo ln -s /etc/nginx/sites-available/websocker-fiber.conf /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl restart nginx
```

Ahora si vas a tu dominio deberias tener el chat a travez de **wss** y conn **https**.
