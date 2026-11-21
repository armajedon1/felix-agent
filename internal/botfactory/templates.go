package botfactory

import "time"

func TemplatePizzeria() *BotSpec {
	return &BotSpec{
		ID:          "pizzeria-napoli",
		Name:        "Pizzeria Napoli",
		Type:        TypeOrderTaking,
		Description: "Bot de atencion al cliente y toma de pedidos para pizzeria artesanal.",
		SystemPrompt: `Eres el recepcionista virtual de Pizzeria Napoli. Tu objetivo es atender clientes con calidez, tomar sus pedidos y confirmar la orden de manera eficiente y precisa.
Guiate siempre por el catalogo oficial de productos y precios vigentes. Si el cliente pide algo que no esta en el catalogo, aclarale respetuosamente que no disponemos de ese producto.`,
		StrictGrounding: true,
		FallbackMessage: "Disculpa, no dispongo de esa informacion o producto en nuestra carta. Si necesitas algo especial por favor avisanos.",
		BusinessHours: &BusinessHours{
			Days:                []string{"Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"},
			OpenTime:            "19:30",
			CloseTime:           "23:30",
			SlotDurationMinutes: 15,
		},
		RequiredSlots: []string{"items", "customer_name", "delivery_address", "payment_method"},
		Catalog: []CatalogItem{
			{ID: "piz-muz", Name: "Pizza Muzzarella", Category: "Pizzas", Price: 8000, Description: "Salsa de tomate, abundante muzzarella, oregano y aceitunas", Available: true},
			{ID: "piz-nap", Name: "Pizza Napolitana", Category: "Pizzas", Price: 9500, Description: "Muzzarella, rodajas de tomate fresco, ajo y albahaca", Available: true},
			{ID: "piz-fug", Name: "Pizza Fugazzeta", Category: "Pizzas", Price: 9000, Description: "Cebolla caramelizada, abundante muzzarella y oregano", Available: true},
			{ID: "piz-cal", Name: "Pizza Calabresa", Category: "Pizzas", Price: 9800, Description: "Muzzarella y rodajas de longaniza calabresa", Available: true},
			{ID: "emp-car", Name: "Empanada de Carne Suave", Category: "Empanadas", Price: 1200, Description: "Carne cortada a cuchillo, cebolla y especias", Available: true},
			{ID: "emp-jyq", Name: "Empanada Jamon y Queso", Category: "Empanadas", Price: 1200, Description: "Jamon cocido seleccionado y queso muzzarella", Available: true},
			{ID: "beb-coc", Name: "Coca Cola 1.5L", Category: "Bebidas", Price: 3000, Description: "Botella linea original", Available: true},
			{ID: "beb-cer", Name: "Cerveza Quilmes 1L", Category: "Bebidas", Price: 3500, Description: "Botella descartable", Available: true},
		},
		FAQ: []FAQItem{
			{ID: "faq-env", Question: "Hacen envios a domicilio?", Answer: "Si, contamos con delivery propio con una demora promedio de 30 a 45 minutos.", Keywords: []string{"envio", "delivery", "demora"}},
			{ID: "faq-pag", Question: "Que medios de pago aceptan?", Answer: "Aceptamos efectivo al entregar y transferencia o dinero en cuenta por Mercado Pago.", Keywords: []string{"pago", "efectivo", "transferencia", "tarjeta", "alias"}},
			{ID: "faq-tac", Question: "Tienen opciones sin TACC o veganas?", Answer: "Por el momento no contamos con cocina aislada para certificacion sin TACC ni quesos vegetales.", Keywords: []string{"tacc", "celiaco", "vegano"}},
		},
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TemplateAppointments() *BotSpec {
	return &BotSpec{
		ID:          "centro-estetica-peluqueria",
		Name:        "Centro de Estetica y Peluqueria",
		Type:        TypeAppointmentBooking,
		Description: "Bot de asignacion y gestion de turnos para salon de estetica y estilismo.",
		SystemPrompt: `Eres el asistente de citas y turnos del Centro de Estetica y Peluqueria. Tu funcion es coordinar turnos segun los servicios disponibles y la disponibilidad horaria del salon.
Pregunta el nombre, telefono, servicio deseado, fecha y horario. No reserves fechas ni horas fuera de los horarios de atencion.`,
		StrictGrounding: true,
		FallbackMessage: "Lo siento, no puedo agendar turnos fuera de nuestros horarios habituales o para servicios no listados.",
		BusinessHours: &BusinessHours{
			Days:                []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
			OpenTime:            "09:00",
			CloseTime:           "19:00",
			SlotDurationMinutes: 30,
		},
		RequiredSlots: []string{"customer_name", "customer_phone", "service", "date", "time"},
		Catalog: []CatalogItem{
			{ID: "srv-cor", Name: "Corte y Estilismo", Category: "Peluqueria", Price: 7000, Description: "Lavado, corte unisex y peinado final", Available: true},
			{ID: "srv-bar", Name: "Perfilado de Barba", Category: "Barberia", Price: 5000, Description: "Afeitado tradicional con toalla caliente", Available: true},
			{ID: "srv-tin", Name: "Coloracion Completa", Category: "Peluqueria", Price: 16000, Description: "Aplicacion de tintura profesional", Available: true},
			{ID: "srv-man", Name: "Manicuria Semipermanente", Category: "Estetica", Price: 9000, Description: "Cuidado de cuticulas y esmaltado duradero", Available: true},
			{ID: "srv-mas", Name: "Masaje Descontracturante (45 min)", Category: "Bienestar", Price: 12000, Description: "Sesion focalizada en espalda y cuello", Available: true},
		},
		FAQ: []FAQItem{
			{ID: "faq-ubi", Question: "Donde estan ubicados?", Answer: "Estamos ubicados en Av. Principal 1234, planta baja.", Keywords: []string{"donde", "direccion", "ubicacion"}},
			{ID: "faq-can", Question: "Como cancelo o reprogramo un turno?", Answer: "Puedes avisarnos por este mismo chat con al menos 3 horas de anticipacion.", Keywords: []string{"cancelar", "reprogramar", "cambiar"}},
		},
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TemplateCallScheduler() *BotSpec {
	return &BotSpec{
		ID:          "agendamiento-llamadas",
		Name:        "Agendador de Llamadas y Demos",
		Type:        TypeAppointmentBooking,
		Description: "Bot de coordinacion de reuniones comerciales y llamadas de consultoria tecnica.",
		SystemPrompt: `Eres el coordinador de agenda de nuestro equipo comercial y tecnico. Tu mision es programar reuniones y llamadas virtuales de 30 o 45 minutos segun la necesidad del interesado.
Recopila nombre, telefono o WhatsApp, correo electronico y el dia y hora de preferencia.`,
		StrictGrounding: true,
		FallbackMessage: "Por favor elige un horario entre lunes y viernes de 10:00 a 18:00 para coordinar la videollamada.",
		BusinessHours: &BusinessHours{
			Days:                []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"},
			OpenTime:            "10:00",
			CloseTime:           "18:00",
			SlotDurationMinutes: 30,
		},
		RequiredSlots: []string{"customer_name", "customer_phone", "service", "date", "time"},
		Catalog: []CatalogItem{
			{ID: "call-demo", Name: "Llamada de Demostracion (30 min)", Category: "Reuniones", Price: 0, Description: "Presentacion en vivo de la plataforma", Available: true},
			{ID: "call-diag", Name: "Diagnostico Tecnico (45 min)", Category: "Reuniones", Price: 0, Description: "Analisis de arquitectura y viabilidad", Available: true},
		},
		FAQ: []FAQItem{
			{ID: "faq-pla", Question: "Por que plataforma se realiza la llamada?", Answer: "Enviamos un enlace de Google Meet al confirmar la reunion.", Keywords: []string{"plataforma", "meet", "zoom", "enlace"}},
		},
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TemplateFAQSupport() *BotSpec {
	return &BotSpec{
		ID:          "soporte-cliente-garantias",
		Name:        "Atencion al Cliente y Garantias",
		Type:        TypeFAQSupport,
		Description: "Bot de resolucion de consultas y atencion al cliente estricta sin alucinaciones.",
		SystemPrompt: `Eres el agente oficial de atencion al cliente de nuestra empresa. Tu responsabilidad es responder consultas sobre envios, compras, garantias y cambios.
REGLA INQUEBRANTABLE: Responde UNICAMENTE con la informacion que figura en las preguntas frecuentes y datos oficiales. Si el cliente pregunta algo que no esta contemplado, responde amablemente que no dispones de esa informacion y ofrece contactar a un operador humano.`,
		StrictGrounding: true,
		FallbackMessage: "No dispongo de esa informacion en nuestra base de conocimiento. Por favor comunicate con nuestro equipo de soporte humano al correo soporte@empresa.com.",
		BusinessHours: &BusinessHours{
			Days:                []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"},
			OpenTime:            "09:00",
			CloseTime:           "18:00",
			SlotDurationMinutes: 0,
		},
		FAQ: []FAQItem{
			{ID: "faq-gar", Question: "Cual es la politica de garantia?", Answer: "Nuestros productos cuentan con 6 meses de garantia oficial ante desperfectos de fabricacion presentando la factura.", Keywords: []string{"garantia", "falla", "rotura", "cambio"}},
			{ID: "faq-dev", Question: "Como solicito una devolucion o reembolso?", Answer: "Dispones de 10 dias corridos desde recibido el paquete para devolucion sin cargo siempre que conserve su embalaje original.", Keywords: []string{"devolucion", "reembolso", "devolver"}},
			{ID: "faq-env", Question: "Cuanto demora el envio y cual es el costo?", Answer: "Despachamos en 24 horas habiles. El envio es sin cargo en compras mayores a $30.000 a todo el pais.", Keywords: []string{"envio", "costo", "demora", "gratis"}},
			{ID: "faq-fac", Question: "Emiten factura A o B?", Answer: "Emitimos factura A y B de manera automatica con los datos fiscales cargados en el pedido.", Keywords: []string{"factura", "afip", "iva"}},
		},
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TemplateComunidad() *BotSpec {
	return &BotSpec{
		ID:          "comunidad",
		Name:        "Asistente Comunitario",
		Nicknames:   []string{"Asistente", "Comunidad"},
		Type:        TypeCustom,
		Description: "Asistente de comunidad y soporte humano en WhatsApp para grupos y miembros.",
		Personality: "Hablás de forma completamente natural, humana y cotidiana (español rioplatense, tono cercano, relajado y sencillo). Nada de sonar rígido o corporativo. Cero jerga técnica o informática (prohibido decir backend, buffer, servidor, código, sesión, API). Cero robótico o ceremonioso, calidez directa al grano.",
		SystemPrompt: `Eres un asistente de comunidad y acompañamiento en WhatsApp.

Reglas fundamentales de personalidad y comunicacion:
1. Habla de forma completamente natural, humana y cotidiana: Expresate como una persona normal en un grupo de WhatsApp de barrio o comunidad en Argentina (espanol rioplatense, tono cercano, relajado y sencillo). Nada de sonar rigido o corporativo.
2. PROHIBICION TOTAL DE JERGA TECNICA E INFORMATICA: Jamas uses palabras como "backend", "buffer", "servidor", "codigo", "sesion", "API", "memoria tecnica", "sistema" ni justificaciones computacionales. Si algo salio repetido o hubo una falla, responde con naturalidad cotidiana (por ejemplo: "Se me envio dos veces sin querer").
3. Cero robotico o ceremonioso: Cero saludos extensos ni formulas de cierre ("Quedo a su disposicion...", "En que mas puedo ayudarles?"). Anda directo y con calidez al grano.
4. Formato limpio para WhatsApp: No uses numerales (#) ni encabezados markdown. Para destacar nombres, fechas o datos clave usa negrita con un solo asterisco (*asi*). NUNCA agregues prefijos, etiquetas ni corchetes al inicio de tu mensaje.
5. Respuestas breves y al grano: Maximo 2 o 3 oraciones cortas en general, salvo cuando te pidan explicitamente una lista detallada o reporte.
6. Ortografia y caracteres en espanol: Respeta tildes y especialmente la letra "ñ" (nunca escribas "anos" por "años").
7. Sistema de recordatorios y avisos: Si te piden agendar o programar un recordatorio o aviso para una actividad o reunion, confirma con naturalidad que queda agendado.`,
		StrictGrounding: false,
		AllowGroups:     true,
		AllowAllUsers:   false,
		AllowedUsers:    []string{},
		AllowedGroups:   []string{},
		GroupAliases:    map[string]string{},
		Model:           "gemini-2.5-flash",
		Port:            8082,
		Status:          "active",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
}
