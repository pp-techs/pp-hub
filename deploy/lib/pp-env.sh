# pp-env.sh — shared by the deploy scripts; sourced, not run.
#
# pp_set_env FILE KEY VALUE
#   Sets KEY=VALUE in an env file: replaces the first `KEY=` line, or the
#   first commented-out `# KEY=` line (so a value lands next to the comment
#   that explains it), and appends when neither exists. Everything else in
#   the file — comments included — is left as it was.
pp_set_env() {
	_file=$1 _key=$2 _value=$3
	if grep -Eq "^(# ?)?$_key=" "$_file"; then
		_tmp=$(mktemp)
		awk -v k="$_key" -v line="$_key=$_value" '
			!done && ($0 ~ "^" k "=" || $0 ~ "^# ?" k "=") { print line; done = 1; next }
			{ print }
		' "$_file" >"$_tmp"
		cat "$_tmp" >"$_file"
		rm -f "$_tmp"
	else
		printf '%s=%s\n' "$_key" "$_value" >>"$_file"
	fi
}
