import {spawn} from 'node:child_process';
import {createServer} from 'node:http';
import {mkdir,writeFile,readFile} from 'node:fs/promises';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
const root=resolve('work/pi-verification');
await mkdir(root,{recursive:true});
const out=resolve('outputs');
await mkdir(out,{recursive:true});
const results=[],clients=[],requests=[];let failOnce=false;
const delay=ms=>new Promise(r=>setTimeout(r,ms));
const server=createServer(async(req,res)=>{
 let body='';for await(const x of req)body+=x;
 const data=JSON.parse(body);requests.push(data);
 const last=data.messages.at(-1);const str=JSON.stringify(last.content);
 if(str.includes('FAIL_ONCE')&&!failOnce){failOnce=true;res.writeHead(503,{'Content-Type':'application/json'});res.end(JSON.stringify({error:{message:'Local temporary overload',type:'server_error'}}));return;}
 if(str.includes('FAIL_FINAL')){res.writeHead(400,{'Content-Type':'application/json'});res.end(JSON.stringify({error:{message:'Local invalid request',type:'invalid_request_error'}}));return;}
 res.writeHead(200,{'Content-Type':'text/event-stream'});
 const send=(delta,finish_reason=null)=>res.write('data: '+JSON.stringify({id:'test',object:'chat.completion.chunk',created:1,model:data.model,choices:[{index:0,delta,finish_reason}]})+'\n\n');
 send({role:'assistant'});
 if(last.role==='user'&&str.includes('READ_FIXTURE')){
  send({tool_calls:[{index:0,id:'read_'+requests.length,type:'function',function:{name:'read',arguments:JSON.stringify({path:root+'/sample.txt'})}}]});send({},'tool_calls');
 }else if(last.role==='user'&&str.includes('TOOL')){
  send({tool_calls:[{index:0,id:'call_'+requests.length,type:'function',function:{name:'probe_delay',arguments:'{}'}}]});send({},'tool_calls');
 }else{
  await delay(str.includes('SLOW')?1500:100);
  if(!res.destroyed){send({content:'LOCAL_OK'});send({},'stop');}
 }
 if(!res.destroyed)res.end('data: [DONE]\n\n');
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const port=server.address().port;
await mkdir(root+'/config/skills/probe',{recursive:true});await mkdir(root+'/config/prompts',{recursive:true});await mkdir(root+'/config/extensions',{recursive:true});
await writeFile(root+'/config/models.json',JSON.stringify({providers:{probe:{baseUrl:`http://127.0.0.1:${port}/v1`,api:'openai-completions',apiKey:'local-test-only',models:['probe-a','probe-b'].map(id=>({id,input:['text','image'],contextWindow:32000,maxTokens:1000}))}}}));
await writeFile(root+'/config/settings.json',JSON.stringify({defaultProvider:'probe',defaultModel:'probe-a',enableSkillCommands:true,retry:{enabled:true,maxRetries:1,baseDelayMs:25,provider:{maxRetries:0}}}));
await writeFile(root+'/sample.txt','FILE_CONTENT_VERIFIED');
await writeFile(root+'/config/skills/probe/SKILL.md','---\nname: probe\ndescription: Local verification skill\n---\nReply with LOCAL_OK.\n');
await writeFile(root+'/config/prompts/probe-template.md','---\ndescription: Local template\n---\nTEMPLATE_EXPANDED $@\n');
await writeFile(root+'/config/extensions/probe.ts',`import {Type} from '@sinclair/typebox';
export default function(pi){
 pi.registerTool({name:'probe_delay',label:'Probe delay',description:'Local deterministic test',parameters:Type.Object({}),async execute(id,args,signal,onUpdate){onUpdate({content:[{type:'text',text:'progress'}],details:{}});await new Promise(r=>setTimeout(r,900));return {content:[{type:'text',text:'finished'}],details:{}};}});
 pi.registerCommand('probe-dialog',{description:'Local dialog',handler:async(a,ctx)=>{const result=await ctx.ui.confirm('Probe','Continue?');ctx.ui.notify(result?'confirmed':'cancelled','info');}});
 pi.registerCommand('probe-custom',{description:'TUI custom',handler:async(a,ctx)=>{const result=await ctx.ui.custom(()=>{throw Error('must not run')});ctx.ui.notify('custom:'+String(result),'info');}});
 pi.registerCommand('probe-dialogs',{description:'All dialogs',handler:async(a,ctx)=>{const s=await ctx.ui.select('Choice',['A','B']);const i=await ctx.ui.input('Input','text');const e=await ctx.ui.editor('Editor','initial');ctx.ui.notify(JSON.stringify({s,i,e}),'info');}});
}`);
class Client{
 constructor(name,extra=[],config=root+'/config'){
  this.events=[];this.stderr='';this.seq=0;
  const fixtures=config===root+'/config'?['-e',root+'/config/extensions/probe.ts','--skill',root+'/config/skills/probe','--prompt-template',root+'/config/prompts/probe-template.md']:[];
  this.p=spawn('pi',['--mode','rpc','--offline','--no-context-files','--no-approve','--no-extensions','--no-skills','--no-prompt-templates',...fixtures,'--session-dir',root+'/sessions',...extra],{cwd:root,env:{PATH:process.env.PATH,HOME:process.env.HOME,PI_CODING_AGENT_DIR:config,PI_OFFLINE:'1',PI_TELEMETRY:'0'},stdio:['pipe','pipe','pipe']});
  clients.push(this);let buf='';this.p.stdout.on('data',x=>{buf+=x;let i;while((i=buf.indexOf('\n'))>=0){let l=buf.slice(0,i);buf=buf.slice(i+1);try{this.events.push(JSON.parse(l));}catch{}}});this.p.stderr.on('data',x=>this.stderr+=x);
 }
 async wait(fn,start=0,timeout=12000){const end=Date.now()+timeout;while(Date.now()<end){const e=this.events.slice(start).find(fn);if(e)return e;if(this.p.exitCode!==null)throw Error('Pi exit '+this.p.exitCode+' '+this.stderr.slice(0,500));await delay(15);}throw Error('timeout '+JSON.stringify(this.events.slice(-4)));}
 async rpc(type,fields={}){const id=String(++this.seq);this.p.stdin.write(JSON.stringify({id,type,...fields})+'\n');return this.wait(e=>e.type==='response'&&e.id===id);}
 async prompt(message,fields={}){const i=this.events.length;const r=await this.rpc('prompt',{message,...fields});assert.equal(r.success,true);await this.wait(e=>e.type==='agent_settled',i);return this.events.slice(i);}
 async stop(){if(this.p.exitCode!==null)return;this.p.kill('SIGTERM');await Promise.race([new Promise(r=>this.p.once('exit',r)),delay(2000)]);if(this.p.exitCode===null)this.p.kill('SIGKILL');}
}
async function test(name,fn){try{const detail=await fn();results.push({name,status:'PASS',detail});}catch(e){results.push({name,status:'FAIL',detail:String(e)});}console.log(JSON.stringify(results.at(-1)));}
try{
 const a=new Client('a');
 await test('RPC startup and model discovery/switch',async()=>{const s=await a.rpc('get_state');assert.equal(s.success,true);const m=await a.rpc('get_available_models');assert.equal(m.data.models.length,2);assert.equal((await a.rpc('set_model',{provider:'probe',modelId:'probe-b'})).success,true);return 'Two configured models discovered; switching succeeds.';});
 await test('Streaming, persistence and isolated session directory',async()=>{const ev=await a.prompt('HELLO');assert(ev.some(e=>e.type==='message_update'));assert(ev.some(e=>e.type==='agent_settled'));const s=(await a.rpc('get_state')).data;assert(s.sessionFile.startsWith(root+'/sessions/'));await readFile(s.sessionFile);return {sessionId:s.sessionId,events:[...new Set(ev.map(e=>e.type))]};});
 await test('Tool lifecycle and coarse status',async()=>{const ev=await a.prompt('TOOL');for(const t of ['tool_execution_start','tool_execution_update','tool_execution_end'])assert(ev.some(e=>e.type===t));return 'Start/update/end present; details can be hidden.';});
 await test('Follow-up and steering ordering',async()=>{const i=a.events.length;await a.rpc('prompt',{message:'TOOL queue test'});await a.wait(e=>e.type==='tool_execution_start',i);assert.equal((await a.rpc('follow_up',{message:'QUEUED_FOLLOW'})).success,true);assert.equal((await a.rpc('steer',{message:'INSERT_STEER'})).success,true);await a.wait(e=>e.type==='agent_settled',i);const ev=a.events.slice(i);const users=ev.filter(e=>e.type==='message_start'&&e.message?.role==='user').map(e=>JSON.stringify(e.message.content));assert(users.findIndex(x=>x.includes('INSERT_STEER'))<users.findIndex(x=>x.includes('QUEUED_FOLLOW')));assert(ev.findIndex(e=>e.type==='tool_execution_end')<ev.findIndex(e=>e.type==='message_start'&&JSON.stringify(e.message?.content).includes('INSERT_STEER')));return {users,queueEvents:ev.filter(e=>e.type==='queue_update')};});
 await test('clear_queue availability',async()=>{const r=await a.rpc('clear_queue');assert.equal(r.success,false);assert.match(r.error,/Unknown command/);return 'Verified unsupported on 0.84.1; do not use newest-main RPC assumptions.';});
 await test('Client-owned queued message promotion',async()=>{const queue=['PROMOTE_ONLY','REMAIN_QUEUED'];const i=a.events.length;await a.rpc('prompt',{message:'TOOL client queue'});await a.wait(e=>e.type==='tool_execution_start',i);const message=queue.shift();assert.equal((await a.rpc('steer',{message})).success,true);await a.wait(e=>e.type==='agent_settled',i);await a.prompt(queue.shift());const users=a.events.slice(i).filter(e=>e.type==='message_start'&&e.message?.role==='user').map(e=>JSON.stringify(e.message.content));assert.equal(users.filter(x=>x.includes('PROMOTE_ONLY')).length,1);assert.equal(users.filter(x=>x.includes('REMAIN_QUEUED')).length,1);return users;});
 await test('Image payload reaches provider',async()=>{await a.prompt('IMAGE',{images:[{type:'image',mimeType:'image/png',data:'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3WQAAAAASUVORK5CYII='}]});assert(JSON.stringify(requests.at(-1).messages).includes('data:image/png;base64,'));return 'Image preserved across real Pi RPC/provider conversion; no vision quality tested.';});
 await test('Arbitrary files field is not attachment API',async()=>{await a.prompt('FILE_FIELD',{files:[{path:root+'/sample.txt'}]});assert(!JSON.stringify(requests.at(-1).messages).includes('sample.txt'));return 'Unknown files field ignored; generic file attachments require path/text handoff.';});
 await test('Local file path handoff and real read tool',async()=>{const ev=await a.prompt('READ_FIXTURE '+root+'/sample.txt');assert(ev.some(e=>e.type==='tool_execution_end'&&e.toolName==='read'&&!e.isError&&JSON.stringify(e.result).includes('FILE_CONTENT_VERIFIED')));assert(JSON.stringify(requests.at(-1).messages).includes('FILE_CONTENT_VERIFIED'));return 'Real built-in read tool reads the managed file and sends contents to the model.';});
 await test('Commands discovery and prompt/skill invocation',async()=>{const cmds=(await a.rpc('get_commands')).data.commands;for(const n of ['probe-dialog','probe-template','skill:probe'])assert(cmds.some(x=>x.name===n));await a.prompt('/probe-template ARGUMENT');assert(JSON.stringify(requests.at(-1)).includes('TEMPLATE_EXPANDED ARGUMENT'));await a.prompt('/skill:probe');assert(JSON.stringify(requests.at(-1)).includes('Reply with LOCAL_OK'));return cmds.map(x=>({name:x.name,source:x.source}));});
 await test('Blocking dialog and unsupported TUI custom',async()=>{const i=a.events.length;const pending=a.rpc('prompt',{message:'/probe-dialog'});const e=await a.wait(e=>e.type==='extension_ui_request'&&e.method==='confirm',i);a.p.stdin.write(JSON.stringify({type:'extension_ui_response',id:e.id,confirmed:true})+'\n');assert.equal((await pending).success,true);await a.wait(e=>e.method==='notify'&&e.message==='confirmed',i);await a.rpc('prompt',{message:'/probe-custom'});await a.wait(e=>e.method==='notify'&&e.message==='custom:undefined',i);return 'confirm roundtrip works; custom() returns undefined.';});
 await test('Select/input/editor interaction roundtrips',async()=>{const i=a.events.length;const pending=a.rpc('prompt',{message:'/probe-dialogs'});for(const [method,value]of [['select','B'],['input','typed'],['editor','edited']]){const e=await a.wait(e=>e.type==='extension_ui_request'&&e.method===method,i);a.p.stdin.write(JSON.stringify({type:'extension_ui_response',id:e.id,value})+'\n');}assert.equal((await pending).success,true);await a.wait(e=>e.method==='notify'&&e.message===JSON.stringify({s:'B',i:'typed',e:'edited'}),i);return 'All three dialogs completed through RPC without a terminal.';});
 await test('Concurrent processes and history isolation',async()=>{const b=new Client('b');await b.rpc('get_state');await Promise.all([a.prompt('SLOW_A'),b.prompt('SLOW_B')]);const am=(await a.rpc('get_messages')).data.messages,bm=(await b.rpc('get_messages')).data.messages;assert(!JSON.stringify(am).includes('SLOW_B'));assert(!JSON.stringify(bm).includes('SLOW_A'));await b.stop();return 'Two real Pi processes run concurrently with distinct sessions.';});
 await test('Rename and restart continuation',async()=>{await a.rpc('set_session_name',{name:'Compatibility probe'});const s=(await a.rpc('get_state')).data;const before=(await a.rpc('get_entries')).data.entries;await a.stop();const c=new Client('resume',['--session',s.sessionFile]);const state=(await c.rpc('get_state')).data;assert.equal(state.sessionId,s.sessionId);assert.equal(state.sessionName,'Compatibility probe');assert.equal((await c.rpc('get_entries')).data.entries.length,before.length);await c.prompt('RESUMED');const i=c.events.length;await c.rpc('prompt',{message:'SLOW_ABORT'});await c.wait(e=>e.type==='agent_start',i);assert.equal((await c.rpc('abort')).success,true);assert.equal((await c.rpc('get_state')).data.isStreaming,false);await c.stop();return 'Session ID/name/history retained; continued after restart; abort returns idle.';});
 await test('Unconfigured Agent startup',async()=>{await mkdir(root+'/empty-config',{recursive:true});const c=new Client('empty',[],root+'/empty-config');const state=await c.rpc('get_state');const models=await c.rpc('get_available_models');assert.equal(state.success,true);assert.equal(models.data.models.length,0);const r=await c.rpc('prompt',{message:'NO_AUTH'});assert.equal(r.success,false);await c.stop();return {availableModels:0,error:r.error};});
 await test('Retry does not signal premature completion',async()=>{const c=new Client('retry');await c.rpc('get_state');const ev=await c.prompt('FAIL_ONCE');assert(ev.some(e=>e.type==='auto_retry_start'));assert.equal(ev.filter(e=>e.type==='agent_settled').length,1);assert(ev.findIndex(e=>e.type==='auto_retry_end')<ev.findIndex(e=>e.type==='agent_settled'));await c.stop();return [...new Set(ev.map(e=>e.type))];});
 await test('Accepted prompt can still fail',async()=>{const c=new Client('error');await c.rpc('get_state');const ev=await c.prompt('FAIL_FINAL');assert(ev.some(e=>e.type==='message_end'&&e.message?.stopReason==='error'));assert(ev.some(e=>e.type==='agent_settled'));await c.stop();return 'prompt success means acceptance; message stopReason=error distinguishes failure from success.';});
}finally{
 for(const c of clients)await c.stop();server.closeAllConnections();await new Promise(r=>server.close(r));
 await writeFile(out+'/pi-compatibility-results.json',JSON.stringify({version:'0.84.1',date:'2026-09-09',method:'Real local Pi subprocesses; isolated synthetic configuration; loopback deterministic mock model; no real provider credentials',results},null,2));
}
