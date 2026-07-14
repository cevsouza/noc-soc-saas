#!/usr/bin/env bash
# Renderiza os arquivos de configuração a partir de ./templates usando o .env.
# Uso:  ./setup.sh   (depois:  docker compose up -d)
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -f .env ]; then
  echo "ERRO: crie o arquivo .env a partir do .env.example e cole sua chave." >&2
  echo "  cp .env.example .env   # depois edite o NOC_TOKEN" >&2
  exit 1
fi

# shellcheck disable=SC1091
set -a; . ./.env; set +a

if [ -z "${NOC_TOKEN:-}" ] || [ "${NOC_TOKEN}" = "cole_sua_chave_aqui" ]; then
  echo "ERRO: NOC_TOKEN não configurado no .env." >&2
  echo "  Gere a chave em: Cockpit -> Configuração MSP -> Como Conectar (tenant 'noc') -> Gerar chave." >&2
  exit 1
fi
BASE="${NOC_BASE:-https://noc-soc-saas-production.up.railway.app}"
BASE="${BASE%/}"

rm -rf rendered
cp -r templates rendered

# Substitui os placeholders em todos os arquivos renderizados.
find rendered -type f -print0 | while IFS= read -r -d '' f; do
  sed -i.bak "s#__NOC_URL__#${BASE}#g; s#__NOC_TOKEN__#${NOC_TOKEN}#g" "$f"
  rm -f "$f.bak"
done

echo "OK: configs renderizadas em ./rendered apontando para $BASE"
echo
echo "Agora suba o lab:"
echo "  docker compose up -d                     # Prometheus + Alertmanager + Grafana + Uptime Kuma"
echo "  docker compose --profile zabbix up -d    # (opcional) inclui o Zabbix"
