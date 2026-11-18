package main
import (
 "context"
 "fmt"
 "time"
 k "github.com/multica-ai/multica/server/pkg/kagent"
)
func main(){ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel(); c,e:=k.NewA2AClient(k.HTTPConfig{Endpoint:"http://127.0.0.1:18083"});check(e);t,e:=c.Send(ctx,"kagent","multica-agent-35b69a41-efb0-4ec3-96b4-737742c1e3a8",k.NewID(),"","Reply with the single word READY.");check(e);fmt.Printf("send state=%s task=%s session=%s\n",t.Status.State,t.ID,t.ContextID);for !t.Terminal(){select{case <-ctx.Done():panic("timeout");case <-time.After(2*time.Second):};t,e=c.Get(ctx,"kagent","multica-agent-35b69a41-efb0-4ec3-96b4-737742c1e3a8",t.ID,t.ContextID);check(e)};fmt.Printf("terminal=%s answer=%q\n",t.Status.State,t.Text());if t.Status.State!=k.StateCompleted || t.Text()!="READY"{panic("unexpected response")}}
func check(e error){if e!=nil{panic(e)}}
