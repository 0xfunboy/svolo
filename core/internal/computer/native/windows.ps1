# Windows UI Automation provider. Runs in the current interactive user's session,
# never as SYSTEM and never bypasses PowerShell execution policy or UAC.
$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes,WindowsBase,System.Drawing,System.Windows.Forms
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;
public static class SvoloNative {
 [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L,T,R,B; }
 [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X,Y; }
 [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h,out uint pid);
 [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h,out RECT r);
 [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h,out RECT r);
 [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h,ref POINT p);
 [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr h);
 [DllImport("user32.dll")] public static extern short GetAsyncKeyState(int key);
 [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h,IntPtr dc,uint flags);
 [DllImport("user32.dll")] public static extern void SetProcessDPIAware();
 [StructLayout(LayoutKind.Sequential)] struct INPUT { public uint type; public UNION u; }
 [StructLayout(LayoutKind.Explicit)] struct UNION { [FieldOffset(0)] public MOUSEINPUT mi; [FieldOffset(0)] public KEYBDINPUT ki; }
 [StructLayout(LayoutKind.Sequential)] struct MOUSEINPUT { public int dx,dy; public uint data,flags,time;public UIntPtr extra; }
 [StructLayout(LayoutKind.Sequential)] struct KEYBDINPUT { public ushort vk,scan; public uint flags,time;public UIntPtr extra; }
 [DllImport("user32.dll",SetLastError=true)] static extern uint SendInput(uint n,INPUT[] items,int size);
 [DllImport("user32.dll")] static extern bool SetCursorPos(int x,int y);
 public static void Mouse(int x,int y,uint flag,uint data) { if(!SetCursorPos(x,y))throw new Exception("Pointer positioning refused");INPUT i=new INPUT();i.type=0;i.u.mi.flags=flag;i.u.mi.data=data;if(SendInput(1,new INPUT[]{i},Marshal.SizeOf(typeof(INPUT)))!=1)throw new Exception("Mouse input refused (integrity/session boundary)"); }
 public static void Key(ushort key,bool down) {INPUT i=new INPUT();i.type=1;i.u.ki.vk=key;i.u.ki.flags=down?0u:2u;if(SendInput(1,new INPUT[]{i},Marshal.SizeOf(typeof(INPUT)))!=1)throw new Exception("Keyboard input refused (integrity/session boundary)");}
 public static string Capture(IntPtr h,out int width,out int height){RECT r;if(!GetWindowRect(h,out r)||IsIconic(h)||!IsWindowVisible(h))throw new Exception("Target window is not capturable");width=r.R-r.L;height=r.B-r.T;if(width<1||height<1||(long)width*height>12000000)throw new Exception("Window dimensions exceed capture budget");using(Bitmap b=new Bitmap(width,height,PixelFormat.Format24bppRgb)){using(Graphics g=Graphics.FromImage(b)){IntPtr dc=g.GetHdc();try{if(!PrintWindow(h,dc,2))throw new Exception("Application refused scoped window capture");}finally{g.ReleaseHdc(dc);}}using(MemoryStream m=new MemoryStream()){b.Save(m,ImageFormat.Jpeg);return Convert.ToBase64String(m.ToArray());}}}
}
public class SvoloIndicator : System.Windows.Forms.Form {
 public SvoloIndicator(){ FormBorderStyle=System.Windows.Forms.FormBorderStyle.FixedToolWindow;ShowInTaskbar=false;TopMost=true;Width=540;Height=65;StartPosition=System.Windows.Forms.FormStartPosition.Manual;Location=new Point(8,8); }
 protected override bool ShowWithoutActivation {get{return true;}}
 protected override System.Windows.Forms.CreateParams CreateParams {get{var p=base.CreateParams;p.ExStyle|=0x08000000;return p;}}
}
'@ -ReferencedAssemblies System.Drawing,System.Windows.Forms
[SvoloNative]::SetProcessDPIAware()
$token=$env:SVOLO_NATIVE_TOKEN; Remove-Item Env:SVOLO_NATIVE_TOKEN -ErrorAction SilentlyContinue
if ($token.Length -ne 64) { exit 2 }
$refs=@{}; $revision=0; $grants=@{}; $indicator=$null; $cancelled=$false
$denied=@('cmd','powershell','pwsh','WindowsTerminal','conhost','OpenConsole','bash','wsl','mintty','wezterm','alacritty','kitty','consent','LogonUI','CredentialUIBroker','SecurityHealthSystray','winlogon','lsass','KeePass','KeePassXC','1Password','svolo-core','svolo')
$own=@($PID); foreach($n in ($env:SVOLO_OWNER_PIDS -split ',')){if($n -match '^\d+$'){$own += [int]$n}}
function Emit($value){[Console]::WriteLine((ConvertTo-Json -InputObject $value -Compress -Depth 30))}
function Fail([string]$message,[int]$code=-32005){$e=New-Object System.Exception($message);$e.Data['code']=$code;throw $e}
function Apps {
 $result=@()
 foreach($p in (Get-Process)){
  try { if($p.MainWindowHandle -eq [IntPtr]::Zero -or $p.Id -in $own -or $p.ProcessName -in $denied -or $p.ProcessName -match '^pi-?gna'){continue};$path=$p.MainModule.FileName; $sha=[System.Security.Cryptography.SHA256]::Create();try{$id='win32.'+([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($path.ToLowerInvariant())))).Replace('-','').Substring(0,24).ToLowerInvariant()}finally{$sha.Dispose()};$result+=@{id=$id;bundleId=$id;displayName=$p.ProcessName;pid=$p.Id;isRunning=$true;windows=@(@{id=$p.MainWindowHandle.ToInt64();title=$p.MainWindowTitle})} }catch{continue}
 }
 return ,$result
}
function Resolve($ref){$query=$ref; $wantPid=0;if($ref -isnot [string]){$query=$ref.bundleId;$wantPid=$ref.pid};$found=@(Apps | ForEach-Object {$_} | Where-Object {(($_.id -eq $query)-or($_.displayName -eq $query))-and(-not $wantPid -or $_.pid -eq $wantPid)});if($found.Count -ne 1){Fail 'Application missing, denied, or ambiguous. Select an exact identity from list_apps.' -32002};return $found[0]}
function Window($app,$params){$windows=@($app.windows);$handle=$windows[0].id;if($params.window_id){$handle=[long]$params.window_id};$owner=[uint32]0;[void][SvoloNative]::GetWindowThreadProcessId([IntPtr]$handle,[ref]$owner);if($owner -ne $app.pid){Fail 'Window is not owned by the approved application' -32003};return [IntPtr]$handle}
function Approved($app){$g=$grants[$app.id];if(-not $g -or $g.pid -ne $app.pid -or ([DateTime]::UtcNow-$g.last).TotalSeconds -gt 300){Fail 'Application has no current user-approved session' -32001};$g.last=[DateTime]::UtcNow}
function Foreground($handle){if($env:SVOLO_NATIVE_FOREGROUND -ne '1'){Fail 'Foreground input is disabled; explicitly enable it in Computer settings' -32011};if([SvoloNative]::GetForegroundWindow() -ne $handle){Fail 'Target must already be foreground; the agent never activates it' -32011};if([SvoloNative]::GetAsyncKeyState(27) -lt 0){Cancel;Fail 'Stopped by Esc' -32006}}
function Cancel {foreach($id in @($grants.Keys)){Emit @{method='cancelled';params=@{app=$id;session=$grants[$id].session;reason='esc'}}};$grants.Clear();if($script:indicator){$script:indicator.Hide()}}
function Ref($index,$app){$r=$refs[[int]$index];if(-not $r -or $r.pid -ne $app.pid){Fail 'Stale element: get a fresh application snapshot' -32004};if($r.node.Current.ProcessId -ne $app.pid){Fail 'Element owner changed' -32004};if($r.node.Current.IsPassword){Fail 'Password fields are never read or modified' -32007};return $r.node}
function Pattern($node,$id){$p=$null;if(-not $node.TryGetCurrentPattern($id,[ref]$p)){Fail 'Element does not expose the requested semantic pattern' -32011};return $p}
function Snapshot($app,$handle){
 $script:refs=@{};$script:revision++;$root=[System.Windows.Automation.AutomationElement]::FromHandle($handle);$q=New-Object System.Collections.Queue;$q.Enqueue(@{node=$root;depth=0});$lines=New-Object System.Collections.Generic.List[string];$watch=[Diagnostics.Stopwatch]::StartNew();$walker=[System.Windows.Automation.TreeWalker]::ControlViewWalker
 while($q.Count -gt 0 -and $script:refs.Count -lt 1000 -and $watch.Elapsed.TotalSeconds -lt 15){$entry=$q.Dequeue();$node=$entry.node;$depth=$entry.depth;if($depth -gt 24){continue};try{$v=$node.Current;if($v.ProcessId -ne $app.pid){continue};$index=$script:revision*10000+$script:refs.Count+1;$script:refs[$index]=@{node=$node;pid=$app.pid};$name=if($v.IsPassword){'[redacted]'}else{$v.Name};$line=('  '*$depth)+"[$index] "+$v.ControlType.ProgrammaticName+' '+($name|ConvertTo-Json -Compress);if($v.HasKeyboardFocus){$line+=' focused'};$value=$null;if(-not $v.IsPassword -and $node.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern,[ref]$value)){$line+=' value='+($value.Current.Value|ConvertTo-Json -Compress)};$lines.Add($line);$child=$walker.GetFirstChild($node);$count=0;while($child -and $count -lt 1000){$q.Enqueue(@{node=$child;depth=$depth+1});$child=$walker.GetNextSibling($child);$count++}}catch{continue}}
 return @{text=($lines -join "`n");mode='full';bundleId=$app.id;pid=$app.pid;windowId=$handle.ToInt64();focusedWindowTitle=$root.Current.Name;revision=$script:revision;elementCount=$script:refs.Count}
}
function Point($handle,$x,$y){$r=New-Object SvoloNative+RECT;if(-not [SvoloNative]::GetWindowRect($handle,[ref]$r)){Fail 'Window disappeared' -32003};if($x -lt 0 -or $y -lt 0 -or $x -ge ($r.R-$r.L) -or $y -ge ($r.B-$r.T)){Fail 'Coordinates outside approved window' -32008};return @([int]($r.L+$x),[int]($r.T+$y))}
function Call([string]$method,$p){
 if($method -notin @('hello','ping','permissions','request_permissions','open_settings','list_apps','shutdown','overlay_hide','resolve_app','overlay_show','screenshot','get_app_state','set_value','select_text','perform_secondary_action','type_text','paste','press_key','drag','click','scroll')){Fail 'Unknown native method' -32601}
 switch($method){
 'hello' {return @{helperVersion=2;protocol=1;os='windows';arch=$env:PROCESSOR_ARCHITECTURE;pid=$PID;permissions=(Call 'permissions' @{})}}
 'ping' {return @{ok=$true}}
 {$_ -in @('permissions','request_permissions')} {return @{accessibility=[Environment]::UserInteractive;screenRecording=[Environment]::UserInteractive;foregroundEnabled=($env:SVOLO_NATIVE_FOREGROUND -eq '1');note='Runs as the interactive user. Elevated/secure desktops are not supported.'}}
 'open_settings' {Start-Process 'ms-settings:easeofaccess';return @{}}
 'list_apps' {return @{apps=@(Apps | ForEach-Object {$_})}}
 'shutdown' {Cancel;return @{}}
 'overlay_hide' {$id=if($p.app -is [string]){$p.app}else{$p.app.bundleId};if($id){$grants.Remove($id)}else{$grants.Clear()};if($script:indicator){$script:indicator.Hide()};return @{}}
 }
 $app=Resolve $p.app
 if($method -eq 'resolve_app'){return @{bundleId=$app.id;displayName=$app.displayName;pid=$app.pid}}
 if($method -eq 'overlay_show'){
  if(-not $p.session){Fail 'Explicit session required' -32008};if($grants[$app.id] -and $grants[$app.id].session -ne $p.session){Fail 'Application belongs to another session' -32001};if(-not $script:indicator){$script:indicator=New-Object SvoloIndicator;$label=New-Object System.Windows.Forms.Label;$label.Dock='Fill';$label.Name='status';$script:indicator.Controls.Add($label);$script:indicator.Add_FormClosing({param($sender,$e);$e.Cancel=$true;Cancel})};$script:indicator.Controls['status'].Text='svolo: '+$p.session_label+' - '+$app.displayName+' - Esc to stop';$script:indicator.Show();[System.Windows.Forms.Application]::DoEvents();$grants[$app.id]=@{pid=$app.pid;session=$p.session;last=[DateTime]::UtcNow};return @{visible=$true}
 }
 Approved $app;$handle=Window $app $p
 switch($method){
 'get_app_state' {return Snapshot $app $handle}
 'screenshot' {$w=0;$h=0;$jpeg=[SvoloNative]::Capture($handle,[ref]$w,[ref]$h);return @{jpeg=$jpeg;width=$w;height=$h;scale=1}}
 'set_value' {$node=Ref $p.element_index $app;$v=Pattern $node ([System.Windows.Automation.ValuePattern]::Pattern);if($v.Current.IsReadOnly){Fail 'Value is read-only'};$v.SetValue([string]$p.value);return @{method='UI Automation ValuePattern';settled=$true}}
 'perform_secondary_action' {$node=Ref $p.element_index $app;switch(([string]$p.action).ToLowerInvariant()){'invoke'{(Pattern $node ([System.Windows.Automation.InvokePattern]::Pattern)).Invoke()};'toggle'{(Pattern $node ([System.Windows.Automation.TogglePattern]::Pattern)).Toggle()};'expand'{(Pattern $node ([System.Windows.Automation.ExpandCollapsePattern]::Pattern)).Expand()};'collapse'{(Pattern $node ([System.Windows.Automation.ExpandCollapsePattern]::Pattern)).Collapse()};'select'{(Pattern $node ([System.Windows.Automation.SelectionItemPattern]::Pattern)).Select()};default{Fail 'Use invoke/toggle/expand/collapse/select as semantic action' -32008}};return @{method='UI Automation pattern';settled=$true}}
 'select_text' {$node=Ref $p.element_index $app;$pattern=Pattern $node ([System.Windows.Automation.TextPattern]::Pattern);$range=$pattern.DocumentRange;$needle=[string]$p.prefix+[string]$p.text+[string]$p.suffix;$found=$range.FindText($needle,$false,$false);if(-not $found){Fail 'Text not found'};$rest=$range.Clone();$rest.MoveEndpointByRange([System.Windows.Automation.TextPatternRangeEndpoint]::Start,$found,[System.Windows.Automation.TextPatternRangeEndpoint]::End);if($rest.FindText($needle,$false,$false)){Fail 'Text is ambiguous; provide prefix/suffix'};if($p.prefix){[void]$found.MoveEndpointByUnit([System.Windows.Automation.TextPatternRangeEndpoint]::Start,[System.Windows.Automation.TextUnit]::Character,([string]$p.prefix).Length)};if($p.suffix){[void]$found.MoveEndpointByUnit([System.Windows.Automation.TextPatternRangeEndpoint]::End,[System.Windows.Automation.TextUnit]::Character,-([string]$p.suffix).Length)};if($p.selection_type -eq 'cursor_before'){$found.MoveEndpointByRange([System.Windows.Automation.TextPatternRangeEndpoint]::End,$found,[System.Windows.Automation.TextPatternRangeEndpoint]::Start)}elseif($p.selection_type -eq 'cursor_after'){$found.MoveEndpointByRange([System.Windows.Automation.TextPatternRangeEndpoint]::Start,$found,[System.Windows.Automation.TextPatternRangeEndpoint]::End)};$found.Select();return @{method='UI Automation TextPattern';settled=$true}}
 'click' {if($null -ne $p.element_index){$node=Ref $p.element_index $app;(Pattern $node ([System.Windows.Automation.InvokePattern]::Pattern)).Invoke();return @{method='UI Automation InvokePattern';settled=$true}}}
 'scroll' {if($null -ne $p.element_index){$node=Ref $p.element_index $app;$sp=Pattern $node ([System.Windows.Automation.ScrollPattern]::Pattern);$none=[System.Windows.Automation.ScrollAmount]::NoAmount;$up=[System.Windows.Automation.ScrollAmount]::LargeDecrement;$down=[System.Windows.Automation.ScrollAmount]::LargeIncrement;switch($p.direction){'up'{$sp.Scroll($none,$up)};'down'{$sp.Scroll($none,$down)};'left'{$sp.Scroll($up,$none)};'right'{$sp.Scroll($down,$none)};default{Fail 'Invalid scroll direction' -32008}};return @{method='UI Automation ScrollPattern';settled=$true}}}
 }
 Foreground $handle
 switch($method){
 'click' {$xy=Point $handle ([double]$p.x) ([double]$p.y);$buttons=@{left=@(2,4);right=@(8,16);middle=@(32,64)};$name=if($p.mouse_button){$p.mouse_button}else{'left'};$flags=$buttons[$name];if(-not $flags){Fail 'Invalid mouse button' -32008};$count=if($p.click_count){[int]$p.click_count}else{1};if($count -lt 1 -or $count -gt 3){Fail 'click_count must be 1..3' -32008};for($i=0;$i -lt $count;$i++){[SvoloNative]::Mouse($xy[0],$xy[1],$flags[0],0);[SvoloNative]::Mouse($xy[0],$xy[1],$flags[1],0)}}
 'drag' {$from=Point $handle ([double]$p.from_x) ([double]$p.from_y);$to=Point $handle ([double]$p.to_x) ([double]$p.to_y);[SvoloNative]::Mouse($from[0],$from[1],2,0);try{for($i=1;$i -le 12;$i++){Foreground $handle;[SvoloNative]::Mouse([int]($from[0]+($to[0]-$from[0])*$i/12),[int]($from[1]+($to[1]-$from[1])*$i/12),1,0);Start-Sleep -Milliseconds 10}}finally{[SvoloNative]::Mouse($to[0],$to[1],4,0)}}
 'scroll' {$xy=Point $handle ([double]$p.x) ([double]$p.y);$direction=[string]$p.direction;if($direction -notin @('up','down','left','right')){Fail 'Invalid direction' -32008};$pages=if($p.pages){[int]$p.pages}else{1};if($pages -lt 1 -or $pages -gt 10){Fail 'pages must be 1..10' -32008};$amount=120*$pages;if($direction -in @('down','left')){$amount=-$amount};$flag=if($direction -in @('left','right')){4096}else{2048};$unsigned=[BitConverter]::ToUInt32([BitConverter]::GetBytes([int]$amount),0);[SvoloNative]::Mouse($xy[0],$xy[1],$flag,$unsigned)}
 'press_key' {$keys=@{ctrl=17;control=17;shift=16;alt=18;meta=91;super=91;enter=13;tab=9;escape=27;esc=27;backspace=8;delete=46;space=32;left=37;up=38;right=39;down=40;home=36;end=35;pageup=33;pagedown=34};$pressed=@();try{foreach($part in ([string]$p.key -split '\+')){$lower=$part.ToLowerInvariant();$key=$keys[$lower];if(-not $key -and $part.Length -eq 1){$key=[int][char]$part.ToUpperInvariant()};if(-not $key){Fail 'Unknown key' -32008};[SvoloNative]::Key($key,$true);$pressed+=@($key)}}finally{[array]::Reverse($pressed);foreach($key in $pressed){[SvoloNative]::Key($key,$false)}}}
 {$_ -in @('type_text','paste')} {
  $focus=[System.Windows.Automation.AutomationElement]::FocusedElement;if(-not $focus -or $focus.Current.ProcessId -ne $app.pid -or $focus.Current.IsPassword){Fail 'Focus is outside the approved application or on a password field' -32007}
  # Preserve the clipboard where possible; fail rather than silently discarding opaque formats.
  $old=[System.Windows.Forms.Clipboard]::GetDataObject();$copy=New-Object System.Windows.Forms.DataObject
  if($old){foreach($format in $old.GetFormats($false)){$data=$old.GetData($format,$false);if($null -eq $data){Fail 'Cannot safely preserve clipboard format' -32011};$copy.SetData($format,$false,$data)}}
  $text=[string]$p.text;$data=New-Object System.Windows.Forms.DataObject;$data.SetText($text,[System.Windows.Forms.TextDataFormat]::UnicodeText)
  if($p.format -eq 'html'){$data.SetText($text,[System.Windows.Forms.TextDataFormat]::Html)}
  try{[System.Windows.Forms.Clipboard]::SetDataObject($data,$true);[SvoloNative]::Key(17,$true);try{[SvoloNative]::Key(86,$true);[SvoloNative]::Key(86,$false)}finally{[SvoloNative]::Key(17,$false)};Start-Sleep -Milliseconds 150}finally{if($old){[System.Windows.Forms.Clipboard]::SetDataObject($copy,$true)}else{[System.Windows.Forms.Clipboard]::Clear()}}
 }
 default{Fail 'Unknown native method' -32601}
 }
 return @{method='Windows foreground (explicit consent)';settled=$true}
}
# ReadLineAsync allows the UI indicator and Esc guard to keep pumping between calls.
$pending=[Console]::In.ReadLineAsync()
while($true){
 [System.Windows.Forms.Application]::DoEvents();if($grants.Count -gt 0 -and [SvoloNative]::GetAsyncKeyState(27) -lt 0){Cancel}
 if(-not $pending.IsCompleted){Start-Sleep -Milliseconds 30;continue}
 $line=$pending.GetAwaiter().GetResult();if($null -eq $line){break};$request=$null
 try{if($line.Length -gt 2097152){Fail 'Native request too large' -32008};$request=ConvertFrom-Json $line;if($request.token -cne $token){Fail 'Unauthorized native caller' -32600};$result=Call $request.method $request.params;Emit @{jsonrpc='2.0';id=$request.id;result=$result};if($request.method -eq 'shutdown'){break}}
 catch{$code=-32005;$e=$_.Exception;while($e.InnerException){$e=$e.InnerException};if($e.Data.Contains('code')){$code=[int]$e.Data['code']};Emit @{jsonrpc='2.0';id=$request.id;error=@{code=$code;message=$e.Message}}}
 $pending=[Console]::In.ReadLineAsync()
}
if($indicator){$indicator.Dispose()}
