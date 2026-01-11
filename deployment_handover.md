# Kubernetes Deployment Standard (K3s/Hostinger)

This document describes the deployment standard used for the backend, to be replicated for the Frontend or other services.

## Infrastructure Context

- **Provider**: Hostinger VPS
- **Kubernetes Distro**: K3s (Single Node)
- **Ingress Controller**: Traefik (Built-in)
- **Cert Manager**: ClusterIssuer `letsencrypt-prod`
- **Container Registry**: Docker Hub (Public/Private)
- **CI/CD**: GitHub Actions -> `kubectl apply`

## 1. Kubernetes Manifests Structure

Create a folder `k8s/` in the root of the repository.

### `k8s/app.yaml` (Deployment & Service)

- **Deployment**:
  - Replicas: 1 (for now).
  - ImagePullPolicy: `Always`.
  - **Liveness/Readiness Probes**: Essential for zero-downtime deployments.
  - **EnvVars**: Use `envFrom: secretRef` to load sensitive data from a manually created Secret on the server.
- **Service**:
  - Type: `ClusterIP` (Internal only).
  - Port 80 targets the container port.

### `k8s/ingress.yaml` (Ingress)

- **ClassName**: `ingressClassName: traefik`.
- **Annotations**: `cert-manager.io/cluster-issuer: letsencrypt-prod` (to auto-issue SSL).
- **TLS**:
  ```yaml
  tls:
    - hosts:
        - your.domain.com
      secretName: your-app-tls
  ```
- **Host**: Define the specific domain.

## 2. Secrets Management

**DO NOT** commit `.env` files or hardcoded secrets.

1.  **On the Server**: Create a Kubernetes Secret manually.
    ```bash
    kubectl create secret generic <service-name>-env --from-literal=KEY=VALUE ...
    ```
2.  **In `k8s/app.yaml`**: Reference it.
    ```yaml
    envFrom:
      - secretRef:
          name: <service-name>-env
    ```

## 3. CI/CD Workflow (`.github/workflows/deploy.yml`)

The pipeline does NOT use SSH to run docker-compose. It uses `kubectl`.

**Required GitHub Secrets:**

- `DOCKER_USERNAME`: Your Docker Hub user.
- `DOCKER_PASSWORD`: Your Docker Hub token/password.
- `KUBECONFIG`: The contents of `~/.kube/config` (or `/etc/rancher/k3s/k3s.yaml`) from the VPS. **Important:** Change the server IP from `127.0.0.1` to the **VPS Public IP**.

**Workflow Steps:**

1.  **Checkout code**.
2.  **Login to Docker Hub**.
3.  **Build and Push** Docker image.
4.  **Install kubectl** (using `azure/setup-kubectl`).
5.  **Set Kubeconfig** (using `azure/k8s-set-context`).
6.  **Deploy**:
    ```bash
    kubectl apply -f k8s/
    kubectl rollout restart deployment/<deployment-name>
    kubectl rollout status deployment/<deployment-name>
    ```

## 4. Debugging (Cheat Sheet)

If the deployment fails:

- **Check Pod Status**: `kubectl get pods`
- **Inspect Start Errors**: `kubectl describe pod <pod-name>` (Look at "Events" at the bottom).
- **View Logs**: `kubectl logs -l app=<app-label> -f`
