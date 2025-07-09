# Plan de développement : Proxy HTTP et SOCKS5 en Go

Ce document décrit les étapes pour créer un serveur proxy multifonctionnel en Go, capable de gérer les protocoles HTTP/HTTPS et SOCKS5.

## Étape 1 : Cr��er un serveur de test HTTP

Pour valider le fonctionnement du proxy, nous avons besoin d'une cible. Cette étape consiste à créer un serveur web très simple qui servira un fichier de test.

1.  **Créer un répertoire pour le serveur de test :**
    ```bash
    mkdir test_server
    ```
2.  **Créer un fichier de test à télécharger :**
    -   Nom du fichier : `test_server/test.txt`
    -   Contenu : `Bonjour, ceci est un test de téléchargement.`
3.  **Créer le code du serveur de test :**
    -   Nom du fichier : `test_server/main.go`
    -   Logique : Le serveur écoutera sur le port `8081` et servira les fichiers du répertoire courant.
4.  **Vérifier la disponibilité du port :**
    -   Avant de lancer le serveur, toujours s'assurer que le port `8081` est libre.

## Étape 2 : Créer le serveur Proxy

C'est le cœur du projet. Le serveur proxy écoutera sur un port unique (par exemple `8080`) et devra déterminer si la connexion entrante est une requête HTTP ou SOCKS5.

1.  **Créer le fichier principal du proxy :**
    -   Nom du fichier : `main.go`
2.  **Logique du `main.go` :**
    -   **Vérifier la disponibilité du port :** Avant de lancer le serveur, toujours s'assurer que le port `8080` est libre.
    -   Lancer un listener TCP sur le port `8080`.
    -   Dans une boucle, accepter les nouvelles connexions.
    -   Pour chaque connexion, lancer une goroutine `handleConnection`.
    -   `handleConnection` lira le premier octet pour "renifler" le protocole :
        -   Si le premier octet est `0x05`, il s'agit d'une connexion SOCKS5 -> appeler `handleSocks5`.
        -   Sinon, il s'agit d'une connexion HTTP -> appeler `handleHttp`.
3.  **Implémenter `handleHttp` :**
    -   Lire la requête HTTP.
    -   Si la méthode est `CONNECT` (pour HTTPS), établir un tunnel TCP vers la destination, envoyer une réponse `200 OK` au client, puis relayer les données dans les deux sens.
    -   Si c'est une autre méthode (GET, POST, etc.), se connecter à la destination, transférer la requête, puis transférer la réponse au client.
4.  **Implémenter `handleSocks5` :**
    -   Effectuer le handshake SOCKS5 (négociation de la méthode d'authentification). Nous supporterons la méthode `0x00` (aucune authentification).
    -   Lire la requête de connexion du client (commande, adresse et port de destination).
    -   Se connecter à la destination demandée.
    -   Envoyer la réponse SOCKS5 au client.
    -   Si la connexion est réussie, relayer les données dans les deux sens entre le client et la destination.

## Étape 3 : Tester la solution complète

Cette étape consiste à vérifier que tout fonctionne comme prévu en utilisant `curl` et `wget`. Chaque test aura un temps mort de 3 secondes.

1.  **Lancer le serveur de test en arrière-plan.**
2.  **Lancer le serveur proxy en arrière-plan.**
3.  **Exécuter les tests de téléchargement :**
    -   **Test 1 :** `curl` avec le proxy HTTP.
      ```bash
      curl --max-time 3 --proxy http://localhost:8080 http://localhost:8081/test.txt
      ```
    -   **Test 2 :** `curl` avec le proxy SOCKS5.
      ```bash
      curl --max-time 3 --proxy socks5://localhost:8080 http://localhost:8081/test.txt
      ```
    -   **Test 3 :** `wget` avec le proxy HTTP.
      ```bash
      wget --timeout=3 -e "http_proxy=http://localhost:8080" http://localhost:8081/test.txt -O -
      ```
    -   **Test 4 :** `wget` avec le proxy SOCKS5.
      ```bash
      # Note : Le support SOCKS5 natif de wget peut être limité.
      # L'utilisation de la variable d'environnement all_proxy est une approche courante.
      all_proxy=socks5://localhost:8080 wget --timeout=3 http://localhost:8081/test.txt -O -
      ```
4.  **Arrêter les serveurs** pour nettoyer l'environnement.
