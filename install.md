
# Instruction to deploy project on ubuntu

Install nginx <br>
`sudo apt update; sudo apt upgrade; sudo apt install nginx`<br>

Configure Firewall <br>
`sudo ufw allow 'Nginx HTTP'` <br>
`sudo ufw allow 22` <br>
`sudo ufw allow 80` <br>
`sudo ufw allow 443` <br>
`sudo ufw allow ssh` <br>
`sudo ufw allow 8443` <br>
`sudo ufw allow 1194` <br>
`sudo ufw enable` <br>

Add a site to nginx
https://www.digitalocean.com/community/tutorials/how-to-install-nginx-on-ubuntu-18-04

configure nginx for webrtc and websocket here is a example config

```
server {

   server_name   example.com www.example.com meet.example.com;

    location / {
        proxy_http_version             1.1;
        proxy_set_header Upgrade       $http_upgrade;
        proxy_set_header Connection    $connection_upgrade;        
        proxy_set_header Host $host;
        proxy_pass https://localhost:8443;
    }



    listen 443 ssl; # managed by Certbot
    ssl_certificate /etc/letsencrypt/live/meet.example.com/fullchain.pem; # managed by Certbot
    ssl_certificate_key /etc/letsencrypt/live/meet.example.com/privkey.pem; # managed by Certbot
    include /etc/letsencrypt/options-ssl-nginx.conf; # managed by Certbot
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem; # managed by Certbot

}
server {
    if ($host = meet.example.com) {
        return 301 https://$host$request_uri;
    } # managed by Certbot



   server_name   example.com www.example.com meet.example.com;
    listen 80;
    return 404; # managed by Certbot
    }
```

add this to nginx.conf http

```
        map $http_upgrade $connection_upgrade {
            default upgrade;
            ''      close;
        }
```

config nginx to make port 1194 stream reachable from domain  <br>

```
mr alisha
```

run galene binary  <br>
`./galene`<br>
note: we kept it simple you can run galene as service or by docker
