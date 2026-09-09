// @vitest-environment jsdom
import { beforeEach, afterEach, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup, act, within } from "@testing-library/react";
import type { Snapshot } from "./types";
const mock=vi.hoisted(()=>({snapshot:vi.fn(),action:vi.fn(),listener:()=>{}}));
vi.mock("./api",()=>({view:"main",api:{snapshot:mock.snapshot,action:mock.action,save:vi.fn(),subscribe:(fn:()=>void)=>{mock.listener=fn;return ()=>{}}}}));
import { App } from "./App";
function state():Snapshot {
 const current={id:"delete-target",title:"删除回归测试",pinned:false,cwd:"/work",createdAt:"",updatedAt:"",status:"idle",error:"",draft:"",messages:[],queue:[],interaction:null,models:[],model:"",commands:[]};
 return {version:1,currentId:current.id,current,sessions:[current],settings:{shortcut:"Alt+Space",piPath:""},environment:{available:true,version:"0.84.1",piPath:"/pi",error:""},error:""};
}
beforeEach(()=>{
 vi.resetAllMocks();Element.prototype.scrollIntoView=vi.fn();
 // WKWebView does not supply a JavaScript confirm delegate: no usable popup.
 vi.spyOn(window,"confirm").mockReturnValue(false);
 HTMLDialogElement.prototype.showModal=function(){this.open=true};
 HTMLDialogElement.prototype.close=function(){this.open=false};
 mock.snapshot.mockResolvedValue(state());mock.action.mockResolvedValue({...state(),version:2,current:null,currentId:"",sessions:[]});
});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
async function openDelete(){render(<App/>);fireEvent.click(await screen.findByRole("button",{name:"管理 删除回归测试"}));fireEvent.click(screen.getByRole("button",{name:"删除会话"}));return screen.findByRole("alertdialog",{name:"删除会话？"})}
it("WebView没有confirm弹窗时仍显示删除确认，取消不会删除",async()=>{
 await openDelete();expect(window.confirm).not.toHaveBeenCalled();expect(mock.action).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"取消"}));expect(screen.queryByRole("alertdialog")).toBeNull();expect(mock.action).not.toHaveBeenCalled();
});
it("确认删除绑定原会话，等待期间只提交一次，成功移除历史",async()=>{
 let resolve!:(s:Snapshot)=>void;mock.action.mockImplementation(()=>new Promise(r=>{resolve=r}));
 await openDelete();const confirm=screen.getByRole("button",{name:"确认删除"});fireEvent.click(confirm);fireEvent.click(confirm);
 expect(mock.action).toHaveBeenCalledTimes(1);expect(mock.action).toHaveBeenCalledWith("delete",{id:"delete-target"});
 await act(async()=>resolve({...state(),version:2,current:null,currentId:"",sessions:[]}));
 await waitFor(()=>expect(screen.queryByRole("button",{name:"管理 删除回归测试"})).toBeNull());expect(screen.queryByRole("alertdialog")).toBeNull();
});
it("删除失败保留确认框和错误，可重试",async()=>{
 mock.action.mockRejectedValueOnce(new Error("磁盘只读"));await openDelete();fireEvent.click(screen.getByRole("button",{name:"确认删除"}));
 await within(screen.getByRole("alertdialog")).findByText("Error: 磁盘只读");expect(screen.getByRole("alertdialog")).toBeTruthy();
 fireEvent.click(screen.getByRole("button",{name:"确认删除"}));await waitFor(()=>expect(screen.queryByRole("alertdialog")).toBeNull());expect(mock.action).toHaveBeenCalledTimes(2);
});
