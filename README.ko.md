# Agent Profile Manager (APM)

[English](README.md) | [한국어](README.ko.md)

Claude Code 설정을 profile 단위로 관리하고 전환할 수 있는 CLI 도구입니다. Claude Code 재시작 없이 settings, skills, commands, agents, rules, 환경변수를 shell별 또는 global로 바꿀 수 있습니다.

## 왜 필요할까?

Claude Code는 모든 설정을 `~/.claude/`에 저장합니다. 업무용으로 다른 모델을 쓰거나, 클라이언트 프로젝트에 다른 permission이 필요하거나, 개인용으로 별도의 AWS credential을 쓰고 싶을 때 불편합니다. APM으로 각각을 profile로 저장해두고 전환하면 됩니다.

## 어떻게 동작하나?

설정을 두 단계로 나눕니다:

- **Common** — 모든 profile에 공유되는 기본 설정
- **Profile** — 특정 환경에 맞는 override와 추가 설정

Profile을 활성화하면 APM이 common + profile을 deep merge해서 generated directory를 만들고, `CLAUDE_CONFIG_DIR`을 설정합니다. Claude Code는 그 directory를 읽을 뿐, 바뀐 걸 알지 못합니다.

```
~/.config/apm/
├── common/              ← 공유 기본 설정
│   ├── settings.json
│   ├── env              ← 공유 환경변수
│   ├── skills/
│   ├── commands/
│   ├── agents/
│   └── rules/
├── profiles/
│   ├── work/            ← profile별 override
│   │   ├── settings.json
│   │   ├── env          ← profile별 환경변수
│   │   ├── skills/
│   │   └── ...
│   └── personal/
├── generated/           ← merge 결과물 (common + profile)
│   ├── work/
│   └── personal/
└── config.yaml          ← active profile, default 설정
```

## 설치

### Script 설치 (권장)

```bash
curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/install.sh | bash
```

Binary를 다운로드하고 shell 설정 파일(`~/.bashrc`, `~/.bash_profile`, 또는 `~/.zshrc`)에 shell integration(`eval "$(apm init ...)"`)을 추가합니다.

다른 경로에 설치하려면:

```bash
APM_INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/install.sh | bash
```

특정 버전을 설치하려면:

```bash
APM_VERSION=v0.1.0 curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/install.sh | bash
```

### Go install

```bash
go install github.com/paulbkim01/agent-profile-manager@latest
mv "$(go env GOPATH)/bin/agent-profile-manager" "$(go env GOPATH)/bin/apm"
```

### Source에서 build

```bash
git clone https://github.com/paulbkim01/agent-profile-manager.git
cd agent-profile-manager
make install   # 또는: just install
```

### 제거

```bash
curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/uninstall.sh | bash
```

Binary 삭제, shell integration 제거, APM config directory 삭제(선택)를 수행합니다.

## 빠른 시작

### 현재 설정 가져오기

```bash
apm create --current
```

기존 `~/.claude/` 내용으로 "default" profile을 만들고 바로 활성화합니다. 새 profile은 항상 자동 활성화되고, "default"라는 이름이면 새 shell의 global default로도 설정됩니다.

`apm create`를 `--current` 없이 실행해도, profile이 하나도 없고 `~/.claude/settings.json`이 있으면 자동으로 import합니다.

### Profile 추가

```bash
apm create work --description "업무 환경"
apm create personal --from work  # 기존 profile 복제
```

### Profile 전환

```bash
# 현재 shell에서 활성화
eval "$(apm use work)"

# 새 shell의 default로도 설정
eval "$(apm use work --global)"

# 비활성화
eval "$(apm use --unset)"
```

### Shell integration

`~/.bashrc` 또는 `~/.zshrc`에 추가하면 전환이 편해집니다:

```bash
eval "$(apm init bash)"   # 또는: eval "$(apm init zsh)"
```

Shell integration이 있으면 `eval "$(...)"` 없이 `apm use`만으로 전환되고, default profile이 새 shell에서 자동 활성화됩니다.

`apm`을 인자 없이 실행하면 현재 설정 상태를 출력합니다.

## 명령어

| 명령어 | Alias | 설명 |
|--------|-------|------|
| `apm create [name]` | | Profile 생성 (기본: "default") |
| `apm use <profile>` | | Profile 전환 |
| `apm use --unset` | | 비활성화 |
| `apm ls` | `list` | 전체 profile 목록 |
| `apm describe <name>` | | Profile 상세 정보 |
| `apm edit <name>` | | `$EDITOR`로 settings.json 편집 |
| `apm delete <name>` | `rm` | Profile 삭제 |
| `apm diff <a> <b>` | | 두 profile의 차이 비교 |
| `apm rename <old> <new>` | `mv` | Profile 이름 변경 |
| `apm doctor` | | 설정 문제 점검 |
| `apm nuke` | | 전체 profile 제거, common은 유지 |
| `apm current` | | Active profile 이름 출력 |
| `apm init <bash\|zsh>` | | Shell integration script 출력 |

Global flag: `--debug` (상세 로그), `--config-dir <path>` (config directory 변경)

### `apm create`

```bash
apm create                              # "default" 생성, 자동 활성화
apm create work                         # 빈 "work" 생성, 자동 활성화
apm create work --current               # 현재 ~/.claude에서 import
apm create work --from personal         # 기존 profile에서 복사
apm create work --description "내 업무" # 설명 추가
```

### `apm use`

```bash
eval "$(apm use work)"            # 이 shell에서 활성화
eval "$(apm use work --global)"   # 새 shell default로도 설정
eval "$(apm use --unset)"         # 비활성화
```

`eval`로 감싸면 shell export(`APM_PROFILE`, `CLAUDE_CONFIG_DIR`, profile 환경변수)를 출력하고, terminal에서 직접 실행하면 안내 메시지를 출력합니다.

Profile 전환 시 이전 profile의 환경변수가 자동으로 unset되어 profile 간에 격리됩니다.

### `apm edit`

Settings.json을 editor에서 엽니다 (`$VISUAL` > `$EDITOR` > `vi`). 저장하면 JSON validation 후 active profile이면 자동 regenerate합니다.

### 자동 재생성

APM은 source 파일 변경을 감지해서 다음 `apm use` 시 재생성합니다. 수동 regenerate 불필요 — profile을 전환하거나 새 shell을 열면 됩니다.

내부적으로 source 파일 metadata(mtime)의 hash를 `.apm-meta.json`에 저장합니다. 활성화할 때마다 hash를 비교하고, 다르면 재생성합니다. 이 검사는 빠릅니다 (~1ms, stat만 사용).

`apm edit`도 편집한 profile이 active면 자동 재생성합니다.

### `apm nuke`

전체 profile과 generated data를 제거합니다. Common은 유지됩니다.

Active profile이 있으면 generated directory를 `~/.claude`로 flatten합니다 (symlink를 실제 파일로 변환).

```bash
apm nuke           # 확인 후 실행
apm nuke --force   # 확인 생략
```

## Profile별 환경변수

각 profile에서 `env` 파일로 환경변수를 설정할 수 있습니다. Profile 전환 시 자동으로 export되고, 전환하거나 비활성화하면 unset됩니다.

**common/env** — 모든 profile에 공유:
```bash
# 공유 API 설정
ANTHROPIC_MODEL=claude-sonnet-4-20250514
```

**profiles/bedrock/env** — profile별:
```bash
AWS_PROFILE=bedrock-account
AWS_REGION=us-west-2
AWS_BEDROCK_MODEL=us.anthropic.claude-sonnet-4-20250514
```

Env 파일은 generation 시 merge됩니다 (profile 우선). 형식: 줄당 `KEY=VALUE`, `#`으로 주석, 따옴표 선택적.

Profile 전환 시 이전 환경변수가 unset된 후 새 환경변수가 export됩니다.

## Settings merge 규칙

Profile 생성 시 `common/settings.json`과 `profiles/<name>/settings.json`을 deep merge합니다:

| 경우 | 동작 |
|------|------|
| Scalar 값 | Profile 우선 |
| Nested object | Recursive deep merge |
| `permissions.allow` | Union (중복 제거) |
| `enabledPlugins` | Object merge (모든 key 유지) |
| `null`로 설정 | 해당 key 삭제 |
| Type 불일치 | Profile 우선 |

### 예시

**common/settings.json:**
```json
{
  "effortLevel": "high",
  "permissions": {
    "allow": ["Read", "Write", "Edit"]
  },
  "enabledPlugins": {
    "plugin-a@marketplace": true
  }
}
```

**profiles/work/settings.json:**
```json
{
  "model": "us.anthropic.claude-opus-4-6-v1[1m]",
  "permissions": {
    "allow": ["Grep", "WebFetch"]
  },
  "enabledPlugins": {
    "gopls-lsp@marketplace": true
  },
  "voiceEnabled": null
}
```

**결과:**
```json
{
  "effortLevel": "high",
  "model": "us.anthropic.claude-opus-4-6-v1[1m]",
  "permissions": {
    "allow": ["Read", "Write", "Edit", "Grep", "WebFetch"]
  },
  "enabledPlugins": {
    "plugin-a@marketplace": true,
    "gopls-lsp@marketplace": true
  }
}
```

`voiceEnabled`는 null이라 삭제, `permissions.allow`는 union, `enabledPlugins`는 object merge.

## 관리 directory

Profile에 다음 directory를 포함할 수 있습니다. Common과 profile의 파일이 generated output에 symlink로 합쳐지고, 겹치면 profile이 우선합니다:

- `skills/`
- `commands/`
- `agents/`
- `rules/`

관리 항목이 아닌 파일(`CLAUDE.md` 등)도 symlink됩니다.

## Profile별 auth

APM은 `CLAUDE_CONFIG_DIR`를 통해 `.claude.json`을 profile별로 분리합니다. 각 generated directory에 자체 `.claude.json`이 있어서 profile마다 다른 Claude 계정을 쓸 수 있습니다.

`apm create --current`로 만들면 현재 `~/.claude.json`이 generated directory에 복사됩니다. `--current` 없이 만든 profile은 새로 시작합니다.

macOS에서는 OAuth token이 Keychain에 저장되므로, `.claude.json`에는 어떤 계정(`oauthAccount`)을 쓸지만 기록됩니다.

## Profile metadata

각 profile에 `profile.yaml`이 있습니다:
- `name` — profile 이름
- `description` — 설명 (선택)
- `created_at` — RFC 3339 timestamp
- `source` — `""`, `"current"`, 또는 복사 원본 profile 이름

## Profile 이름 규칙

소문자, 숫자, hyphen만 가능. 문자 또는 숫자로 시작. 최대 50자. 예약어: `common`, `generated`, `config`, `current`, `state`.

## 설정

Data는 `~/.config/apm/` (또는 `$XDG_CONFIG_HOME/apm/`)에 저장됩니다.

**config.yaml:**
- `default_profile` — 새 shell의 default profile
- `active_profile` — 현재 active profile
- `claude_dir` — `~/.claude` 경로 override

경로 변경: `apm --config-dir /path/to/dir <command>`

## 개발

```bash
just build       # 또는: make build
just test        # 또는: make test
just dev         # sandbox shell (실제 ~/.claude 안 건드림)
just run ls      # sandbox에서 단일 명령 실행
just vet         # 또는: make vet
```

Dev mode는 `dev` build tag를 사용합니다. Config를 `/tmp/apm-dev-<uid>`로 자동 sandbox합니다.

## 요구 사항

- Go 1.25+ (source build 시)
- bash 또는 zsh
- macOS 또는 Linux
