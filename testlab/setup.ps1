# Renderiza os arquivos de configuração a partir de ./templates usando o .env.
# Uso:  .\setup.ps1   (depois:  docker compose up -d)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

if (-not (Test-Path ".env")) {
  Write-Host "ERRO: crie o arquivo .env a partir do .env.example e cole sua chave." -ForegroundColor Red
  Write-Host "  Copy-Item .env.example .env   ; depois edite o NOC_TOKEN"
  exit 1
}

# Lê o .env (KEY=VALUE), ignorando comentários e linhas vazias.
$cfg = @{}
Get-Content ".env" | ForEach-Object {
  $line = $_.Trim()
  if ($line -and -not $line.StartsWith("#") -and $line.Contains("=")) {
    $k, $v = $line -split '=', 2
    $cfg[$k.Trim()] = $v.Trim()
  }
}

$base  = $cfg['NOC_BASE']
$token = $cfg['NOC_TOKEN']

if (-not $token -or $token -eq 'cole_sua_chave_aqui') {
  Write-Host "ERRO: NOC_TOKEN não configurado no .env." -ForegroundColor Red
  Write-Host "  Gere a chave em: Cockpit -> Configuração MSP -> Como Conectar (tenant 'noc') -> Gerar chave."
  exit 1
}
if (-not $base) { $base = 'https://noc-soc-saas-production.up.railway.app' }
$base = $base.TrimEnd('/')

# Copia templates -> rendered e substitui os placeholders.
if (Test-Path "rendered") { Remove-Item -Recurse -Force "rendered" }
Copy-Item -Recurse "templates" "rendered"

Get-ChildItem -Recurse -File "rendered" | ForEach-Object {
  $content = Get-Content $_.FullName -Raw
  $content = $content.Replace('__NOC_URL__', $base).Replace('__NOC_TOKEN__', $token)
  Set-Content -Path $_.FullName -Value $content -Encoding UTF8 -NoNewline
}

Write-Host "OK: configs renderizadas em ./rendered apontando para $base" -ForegroundColor Green
Write-Host ""
Write-Host "Agora suba o lab:" -ForegroundColor Cyan
Write-Host "  docker compose up -d               # Prometheus + Alertmanager + Grafana + Uptime Kuma"
Write-Host "  docker compose --profile zabbix up -d   # (opcional) inclui o Zabbix"
