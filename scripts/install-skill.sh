#!/usr/bin/env bash
#
# install-skill.sh — install the in-repo `council-config` Agent Skill into the
# native skills directory of one or more AI coding CLIs.
#
# The repo's skills/council-config/ (SKILL.md + reference/) is the single source
# of truth. This script COPIES that directory unchanged into each tool's skills
# location — the same SKILL.md skill for every target, no per-tool reformatting.
#
# Supported targets (all use the SKILL.md "Agent Skills" open standard):
#   claude    Claude Code
#   codex     OpenAI Codex CLI
#   cursor    Cursor
#   copilot   GitHub Copilot
#   opencode  OpenCode
#
# Usage:
#   scripts/install-skill.sh --all
#   scripts/install-skill.sh --target claude,codex,cursor
#   scripts/install-skill.sh --all --scope project
#   scripts/install-skill.sh --list
#   scripts/install-skill.sh --help
#
set -eu

SKILL_NAME="council-config"
ALL_TARGETS="claude codex cursor copilot opencode"

# Resolve the directory this script lives in, then the source skill directory.
# (No symlink resolution; keep it portable and shellcheck-clean.)
unset CDPATH
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(cd -- "${script_dir}/.." && pwd)
src_dir="${repo_root}/skills/${SKILL_NAME}"

# Defaults; overridden by flags.
scope="user"
project_dir="$PWD"
dry_run=0
force=0
do_list=0
targets=""

prog=$(basename -- "$0")

usage() {
  cat <<EOF
${prog} — install the '${SKILL_NAME}' Agent Skill into your AI CLIs.

The skill is copied verbatim from ${repo_root}/skills/${SKILL_NAME}
(SKILL.md + assets) into each tool's native skills directory.

USAGE:
  ${prog} [--all | --target LIST] [--scope user|project] [options]

TARGET SELECTION:
  --all                 Install into every supported target.
  --target LIST         Comma-separated targets from: ${ALL_TARGETS// /, }.

SCOPE:
  --scope user          Install per-user, available in every repo (default).
  --scope project       Install into the project at --project-dir.
  --project-dir DIR     Project root for --scope project (default: current dir).

OTHER:
  --list                Print each target's destination path and exit.
  --dry-run             Print actions without writing anything.
  --force               Overwrite existing skills without backing them up.
  -h, --help            Show this help and exit.

IDEMPOTENCY:
  Re-running is safe. If a destination already matches the source it is left
  untouched. If it differs it is replaced; the previous copy is moved to
  <dir>.bak.<timestamp> first, unless --force is given.

NOTES:
  - Codex only loads skills when 'skills = true' under [features] in
    \$CODEX_HOME/config.toml (default ~/.codex/config.toml).
  - This script never edits AGENTS.md, .gitignore, or any tool config.
EOF
}

err() {
  printf '%s: %s\n' "$prog" "$1" >&2
}

# Map a target + scope to its destination skill directory.
# Prints the absolute destination directory, or nothing for an unknown target.
dest_for() {
  target="$1"
  home="${HOME:-}"
  codex_home="${CODEX_HOME:-${home}/.codex}"
  xdg_config="${XDG_CONFIG_HOME:-${home}/.config}"

  if [ "$scope" = "project" ]; then
    case "$target" in
      claude)   printf '%s/.claude/skills/%s' "$project_dir" "$SKILL_NAME" ;;
      codex)    printf '%s/.agents/skills/%s' "$project_dir" "$SKILL_NAME" ;;
      cursor)   printf '%s/.cursor/skills/%s' "$project_dir" "$SKILL_NAME" ;;
      copilot)  printf '%s/.github/skills/%s' "$project_dir" "$SKILL_NAME" ;;
      opencode) printf '%s/.opencode/skills/%s' "$project_dir" "$SKILL_NAME" ;;
      *)        return 1 ;;
    esac
  else
    case "$target" in
      claude)   printf '%s/.claude/skills/%s' "$home" "$SKILL_NAME" ;;
      codex)    printf '%s/skills/%s' "$codex_home" "$SKILL_NAME" ;;
      cursor)   printf '%s/.cursor/skills/%s' "$home" "$SKILL_NAME" ;;
      copilot)  printf '%s/.copilot/skills/%s' "$home" "$SKILL_NAME" ;;
      opencode) printf '%s/opencode/skills/%s' "$xdg_config" "$SKILL_NAME" ;;
      *)        return 1 ;;
    esac
  fi
}

# True when the destination already holds a byte-identical copy of the source.
same_as_source() {
  dest="$1"
  [ -d "$dest" ] || return 1
  src_list=$(mktemp) || return 1
  dest_list=$(mktemp) || {
    rm -f "$src_list"
    return 1
  }

  (cd "$src_dir" && find . -type f | sed 's|^\./||' | sort) >"$src_list"
  (cd "$dest" && find . -type f | sed 's|^\./||' | sort) >"$dest_list"

  # Source and destination file lists must match exactly; otherwise stale
  # destination files would survive and violate the "copied verbatim" promise.
  if ! cmp -s "$src_list" "$dest_list"; then
    rm -f "$src_list" "$dest_list"
    return 1
  fi

  # Any source file differing in the destination => not identical.
  same=1
  while IFS= read -r rel; do
    if ! cmp -s "${src_dir}/${rel}" "${dest}/${rel}"; then
      same=0
      break
    fi
  done <"$src_list"

  rm -f "$src_list" "$dest_list"
  [ "$same" -eq 1 ] || return 1
  return 0
}

install_one() {
  target="$1"
  dest=$(dest_for "$target") || { err "unknown target: ${target}"; return 1; }
  parent=$(dirname -- "$dest")

  if same_as_source "$dest"; then
    printf '  %-9s up to date  (%s)\n' "$target" "$dest"
    return 0
  fi

  if [ "$dry_run" -eq 1 ]; then
    if [ -e "$dest" ]; then
      printf '  %-9s would update (%s)\n' "$target" "$dest"
    else
      printf '  %-9s would create (%s)\n' "$target" "$dest"
    fi
    return 0
  fi

  if ! mkdir -p -- "$parent"; then
    err "cannot create ${parent}"
    return 1
  fi

  if [ -e "$dest" ]; then
    if [ "$force" -eq 1 ]; then
      rm -rf -- "$dest"
    else
      backup="${dest}.bak.$(date +%Y%m%d-%H%M%S)"
      if ! mv -- "$dest" "$backup"; then
        err "cannot back up ${dest}"
        return 1
      fi
      printf '  %-9s backed up   (%s)\n' "$target" "$backup"
    fi
  fi

  # Copy the skill contents (SKILL.md + reference/, including any dotfiles).
  if ! mkdir -p -- "$dest" || ! cp -R -- "${src_dir}/." "${dest}/"; then
    err "cannot install into ${dest}"
    return 1
  fi
  printf '  %-9s installed   (%s)\n' "$target" "$dest"
}

# --- argument parsing -------------------------------------------------------
while [ "$#" -gt 0 ]; do
  case "$1" in
    --all)
      targets="$ALL_TARGETS"
      ;;
    --target)
      [ "$#" -ge 2 ] || { err "--target needs a value"; exit 2; }
      targets="$targets $(printf '%s' "$2" | tr ',' ' ')"
      shift
      ;;
    --target=*)
      targets="$targets $(printf '%s' "${1#*=}" | tr ',' ' ')"
      ;;
    --scope)
      [ "$#" -ge 2 ] || { err "--scope needs a value"; exit 2; }
      scope="$2"
      shift
      ;;
    --scope=*)
      scope="${1#*=}"
      ;;
    --project-dir)
      [ "$#" -ge 2 ] || { err "--project-dir needs a value"; exit 2; }
      project_dir="$2"
      shift
      ;;
    --project-dir=*)
      project_dir="${1#*=}"
      ;;
    --list)
      do_list=1
      ;;
    --dry-run)
      dry_run=1
      ;;
    --force)
      force=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      err "unknown option: $1 (try --help)"
      exit 2
      ;;
  esac
  shift
done

case "$scope" in
  user|project) ;;
  *) err "invalid --scope '${scope}' (use user|project)"; exit 2 ;;
esac

if [ ! -f "${src_dir}/SKILL.md" ]; then
  err "source skill not found at ${src_dir}/SKILL.md"
  exit 1
fi

# --list: show every target's resolved destination and exit.
if [ "$do_list" -eq 1 ]; then
  printf 'Source: %s\n' "$src_dir"
  printf 'Scope:  %s%s\n' "$scope" "$([ "$scope" = project ] && printf ' (%s)' "$project_dir")"
  for t in $ALL_TARGETS; do
    printf '  %-9s %s\n' "$t" "$(dest_for "$t")"
  done
  exit 0
fi

# Normalize the requested target list (dedupe, validate).
if [ -z "${targets// /}" ]; then
  err "no targets selected; use --all or --target LIST (see --help)"
  exit 2
fi

selected=""
for t in $targets; do
  case " $ALL_TARGETS " in
    *" $t "*) ;;
    *) err "unknown target '${t}' (valid: ${ALL_TARGETS// /, })"; exit 2 ;;
  esac
  case " $selected " in
    *" $t "*) ;;            # already selected
    *) selected="$selected $t" ;;
  esac
done

printf 'Installing %s skill from %s\n' "$SKILL_NAME" "$src_dir"
printf 'Scope: %s%s\n' "$scope" "$([ "$scope" = project ] && printf ' (%s)' "$project_dir")"

rc=0
for t in $selected; do
  install_one "$t" || rc=1
done

# Codex needs the experimental skills feature switched on to load anything.
case " $selected " in
  *" codex "*)
    printf '\nNote: Codex loads skills only when ~/.codex/config.toml has:\n'
    printf '  [features]\n  skills = true\n'
    ;;
esac

if [ "$dry_run" -eq 1 ]; then
  printf '\nDry run only — nothing was written.\n'
fi

exit "$rc"
